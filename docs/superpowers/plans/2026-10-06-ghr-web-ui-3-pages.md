# ghr web UI, part 3: Repositories, Storage, Settings and follow-ups Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: the skill the **Execution:** line names — dr-superpowers:subagent-driven-development for `subagent`, dr-superpowers:executing-plans for `inline`. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Finish the ghr browser UI: the Repositories, Storage and Settings pages with a confirmation before every destructive action, the part 2 review follow-ups, the homelab docs, and the manual acceptance on the runner LXC. The UI then matches the TUI feature for feature.

**Architecture:** The pages extend part 2's React app in `ghr/web/`. New `api` methods and TanStack Query hooks cover the remaining `/api/*` routes, which already exist; no Go handler changes. Three shared pieces carry the pages. `ConfirmDialog` guards destructive actions. `TagField` edits label lists. A draft model (`lib/draft.ts`, `SaveBar`, `RejectedAlert`, `UnsavedGuard`) gives Settings and the repository page the TUI's edit, save or discard, and leave-with-unsaved-changes flow. Per-repository editing lives on a new route, `/repositories/$name`.

**Tech Stack:** darkraise-ui 6.9.6, React 19, Vite 6, Tailwind 4, TypeScript 5.7, @tanstack/react-router 1.170 (`useBlocker`), @tanstack/react-query 5.104, vitest 4 + jsdom 29 + Testing Library, ESLint 9; Go 1.26 for the fixture test; GitHub Actions.

**Spec:** docs/superpowers/specs/2026-10-05-ghr-web-ui-design.md

**Execution:** inline — `claude --model sonnet --effort high` — 2 of 24 tasks are heavy (Task 1, total 5: the shared `request` path; Task 18, total 5: the auth redirects) and are delegated; the self-implemented tasks top out at total 4 (`medium`), raised to `high` because tasks are delegated.
**Plan review:** 2026-10-06 — dr-superpowers:judge-opus — executability 16 / coherence 17 / coverage 18 / assumptions 16 (round 2)

## Global Constraints

- Code repository: `D:/Repositories/Personal/ghr` (github.com/darkraise/ghr). Work in a new worktree, `D:/Repositories/Personal/ghr-web-pages`, on branch `feat/web-pages` from master `be0033b`. Master holds part 2, merged locally and 22 commits ahead of origin. Never commit to master and never push: pushing master publishes a release through `ci.yml`. The owner ruled on 2026-10-06 to hold the push until Task 24's acceptance passes, and the push itself needs the owner's yes. The plan, spec and register live in `D:/Repositories/Personal/homelab`.
- Every command runs from the worktree root in Git Bash with an explicit `timeout`. Web commands use `npm --prefix web …`. npm runs package scripts through `cmd.exe` on Windows, so scripts in `web/package.json` must not use POSIX-only syntax.
- Web checks: `timeout 300 npm --prefix web run lint`, `timeout 300 npm --prefix web run typecheck`, `timeout 600 npm --prefix web test`, `timeout 600 npm --prefix web run build`. Lint warnings are allowed; errors are not. One test file runs with `timeout 300 npm --prefix web test -- <path>`.
- Go checks before each commit that touches Go: `timeout 120 gofmt -l internal cmd web/*.go` (must print nothing), `timeout 300 go vet ./...`, `timeout 400 go test ./...`. CI's `go test -race` cannot run here (no cgo on Windows).
- Never run `vite` (the dev server), `vitest` without `run`, or any watch mode. Kill any process you start.
- Never commit `web/node_modules/` or files under `web/dist/` other than `web/dist/.gitkeep`. After `npm run build`, `git status --porcelain web/dist` must print nothing.
- CSP (spec §2): no inline `<script>`, no external URLs, no `eval`.
- Every request carries `X-GHR: 1` and `credentials: "same-origin"`; `api` in `web/src/api/client.ts` already does this.
- Polling (spec §5 table): token every 5 s on Settings; storage every 5 s on Storage, and every 1 s while `storageBusy` holds; label check every 1 s while its state is `checking`; repository activity every 5 s while shown. Installable versions are fetched when the Install dialog picks a tool and are not polled. Registrations are fetched when the repository page opens and on Refresh. Available repositories are fetched when the Add dialog opens. Queries do not retry (`retry: false`, set in `createQueryClient`).
- UI texts are the TUI's, verbatim, as quoted in each task. A confirmation dialog's title is the TUI's confirmation question. Its buttons are `Cancel` and the action's label.
- Failed actions toast through the query client's `MutationCache` (`errorText`), so an action built on `useMutation` adds no error toast of its own. The two saves (Tasks 8 and 15) and the dialogs that show their error inline (Tasks 7, 13, 16) use plain `async` functions, not `useMutation`, so nothing toasts twice.
- Buttons that act are disabled while the status poll fails (`useStatus().isError`), as the Dashboard's Pause all is. Exempt: the auth actions (Change password, Log out, the login form), which do not go through `/api/status`, and dialogs already open (their submit shows the request's own error). The Runners row buttons keep part 2's behaviour.
- Tests that wait on real timers pass `{ timeout: 10_000 }` as the third argument to `it`.
- Comments: none unless the why is non-obvious. Never reference this plan, a task, the spec or a review.
- Commits: `<type>(<scope>): <subject>`, subject ≤ 50 characters, imperative, no period. One commit per task.
- The Codex executor lane is closed until 2026-10-10: Claude seats only, no `**Executor:**` lines.

## Contracts

**C1 part 2 surface (unchanged unless a task says so):** `web/src/api/types.ts` (types listed in part 2's C4), `web/src/api/client.ts` (`ApiError`, `request`, `setUnauthorizedHandler`, `api`), `web/src/api/hooks.ts` (`POLL_FAST`, `POLL_SLOW`, `keys`, `useStatus`, `useConfig`, `useMetrics`, `useHistory`, `useSteps`, `useContainers`, `useEvents`, `useLogTail`), `web/src/query.ts` (`createQueryClient`, `errorText`), `web/src/lib/format.ts` (`dur`, `ago`, `humanBytes`, `fmtMem`, `maxText`, `isZeroTime`, `clock`, `hhmm`, `dateTime`, `dateTimeSec`, `elapsed`, `series`), `web/src/lib/use-now.ts` (`useNow`), `web/src/components/state-badge.tsx` (`StateBadge`, `stateVariant`), `web/src/test/render.tsx` (`renderApp(path)` → `{ router, queryClient, user, … }`), `web/src/test/query.tsx` (`withQuery`), `web/src/test/fixtures.ts` (`fixtures`, `authedRoutes(over?)`). A toast's text appears twice in the DOM, so tests find toasts with `findAllByText`. Pages render inside `Shell` and import `PageHeader` from `darkraise-ui/layout`.

**C2 types (Task 1, `web/src/api/types.ts`):**
```ts
export interface RunnerLimitsPatch { memory_max?: string; cpu_quota?: string }
export interface RepoPatch { max?: number; warm?: number; labels?: string[]; cleanup_name_prefixes?: string[]; paused?: boolean }
export interface ConfigPatch {
  mode?: string; global_max?: number; poll_interval?: string; start_timeout?: string; idle_timeout?: string
  history_retention?: string; disk_high_water?: number; build_cache_keep?: string; labels?: string[]
  runner_limits?: RunnerLimitsPatch; repos?: Record<string, RepoPatch>
}
export interface AddRepoRequest { name: string; max?: number; labels?: string[]; allow_public: boolean }
export type PruneScope = "standard" | "build-cache-keep" | "build-cache-all" | "dangling-images" | "unused-volumes"
```

**C3 client (Task 1, `web/src/api/client.ts`):** `request` sends a `string` body as `text/plain`, unchanged, and any other defined body as JSON. New `api` members (`seg` is `encodeURIComponent`; void methods resolve on 202/204):

| Member | Request |
|---|---|
| `patchConfig(patch: ConfigPatch): Promise<void>` | `PATCH /api/config` JSON |
| `reload(): Promise<string[]>` | `POST /api/reload` → warnings |
| `availableRepos(signal?): Promise<AvailableRepo[]>` | `GET /api/repos/available` |
| `addRepo(req: AddRepoRequest): Promise<void>` | `POST /api/repos` JSON |
| `removeRepo(name): Promise<void>` | `DELETE /api/repos/{name}` |
| `pauseRepo(name)`, `resumeRepo(name): Promise<void>` | `POST /api/repos/{name}/pause` / `…/resume` |
| `labelCheck(name, signal?): Promise<LabelCheck>` | `GET /api/repos/{name}/label-check` |
| `startLabelCheck(name): Promise<void>` | `POST /api/repos/{name}/label-check` (202) |
| `registrations(name, signal?): Promise<Registration[]>` | `GET /api/repos/{name}/registrations` |
| `deleteRegistration(name, id: number): Promise<void>` | `DELETE /api/repos/{name}/registrations/{id}` |
| `token(signal?): Promise<TokenStatus>` | `GET /api/token` |
| `replaceToken(token: string): Promise<void>` | `PUT /api/token`, body the raw token as `text/plain` |
| `queueRunnerUpdate()`, `cancelRunnerUpdate(): Promise<void>` | `POST` / `DELETE /api/runner-update` |
| `storage(signal?): Promise<Storage>` | `GET /api/storage` |
| `refreshStorage(): Promise<void>` | `POST /api/storage/refresh` (202) |
| `toolchainChoices(tool, signal?): Promise<ToolchainChoice[]>` | `GET /api/toolchains/available?tool=` |
| `installToolchain(tool, version): Promise<void>` | `POST /api/toolchains` `{tool, version}` (202) |
| `installPreset(preset): Promise<void>` | `POST /api/toolchains` `{preset}` (202) |
| `removeToolchain(tool, version): Promise<void>` | `DELETE /api/toolchains/{tool}/{version}` (202) |
| `clearCache(name): Promise<void>` | `POST /api/caches/{name}/clear` (202) |
| `prune(scope: PruneScope): Promise<void>` | `standard` → `POST /api/prune`; other scopes → `POST /api/prune/{scope}` (202) |

Repository activity uses the existing `api.history(name, "", 500)`.

**C4 test helpers (Task 1):** in `web/src/test/api.ts`, `Call` gains `contentType: string | undefined`. A string request body that is not JSON is recorded as the raw string. A route handler may return a `Promise`, which `mockApi` awaits, so a handler returning `new Promise(() => {})` holds a request pending forever. `web/src/test/fixtures.ts`'s `fixtures` gains `storage: Storage`, `token: TokenStatus`, `labelCheck: LabelCheck`, `registrations: Registration[]`, `availableRepos: AvailableRepo[]` and `toolchainChoices: ToolchainChoice[]`, from the committed JSON fixtures:
- **storage:** toolchain `node 22.11.0` (190 MB); other folder `PyPy`; package caches `nuget` (label `NuGet`, present, 3.6 GB, 41000 files) and `cargo` (label `Cargo`, not present); Docker rows `Images` and `Build Cache` (reclaimable 380 MB); build cache type `regular`; current operation `install node 24` with progress `extracting`, 1 queued; recent `clear nuget` ok with message `cleared NuGet (3.6 GB freed)`; last prune `manual build-cache-all` ok with step `all build cache` 6.8 MB.
- **token:** `ok`, rate 4980 of 5000.
- **labelCheck:** `done`, one group with labels `homelab, self-hosted` and jobs `ci / build, ci / test`, count 12.
- **registrations:** `ghr-aaaaaa` (id 5, online, busy, ghr).
- **availableRepos:** `darkmem` (private, configured) and `new-repo` (private).
- **toolchainChoices:** `22.11.0` (lts) and `24.9.0`.

**C5 hooks (Task 2, `web/src/api/hooks.ts`):** `keys` gains `token`, `storage`, `availableRepos` (constant tuples) and `repoActivity(name)`, `labelCheck(name)`, `registrations(name)`, `toolchainChoices(tool)`. New: `ACTIVITY_LIMIT = 500`, `storageBusy(storage?: Storage, status?: Status): boolean`, `useToken()`, `useStorage(status: Status | undefined)`, `useRepoActivity(name: string)`, `useLabelCheck(name: string, enabled: boolean)`, `useRegistrations(name: string, enabled: boolean)`, `useAvailableRepos(enabled: boolean)` and `useToolchainChoices(tool: string, enabled: boolean)`. Each returns a TanStack `UseQueryResult`.

**C6 shared components (Task 3):**
- `web/src/components/confirm-dialog.tsx` exports `interface Confirm { title: string; body?: string; action: string; destructive?: boolean; run: () => void }` and `ConfirmDialog({ confirm, onClose }: { confirm: Confirm | null; onClose: () => void })`. Callers hold `useState<Confirm | null>(null)`. The dialog renders role `alertdialog`, its title is `confirm.title`, and its buttons are `Cancel` and `confirm.action`. Clicking the action calls `run()` once and closes the dialog. The last request's text stays while the dialog closes.
- `web/src/components/tag-field.tsx` exports `TagField({ label, value, onChange, disabled? }: { label: string; value: string[]; onChange: (next: string[]) => void; disabled?: boolean })`. Its text box is named `label`, its add button `Add to <label>`, and each tag's remove button `Remove <tag>`. Enter or the add button adds the trimmed text, ignoring an empty text, text over 100 characters and an exact duplicate.
- `web/src/components/glyph.tsx` (Task 20) exports `Glyph({ symbol, label, className? })`, rendering `<span role="img" aria-label={label}>`.

**C7 durations (Task 4, `web/src/lib/duration.ts`):** `parseDuration(text: string): number | null` (milliseconds; Go `time.ParseDuration` syntax, or a whole number of days such as `30d`), `durationError(name: string, text: string, floorMs: number): string` (`""` when valid), `sameDuration(a: string, b: string): boolean`, and `DAY_MS = 86_400_000`.

**C8 drafts (Task 5):**
- `web/src/lib/draft.ts` exports `type Values = Record<string, unknown>`, `type Equal = (key: string, a: unknown, b: unknown) => boolean`, `sameValue(a, b): boolean`, `plainEqual: Equal`, `changedKeys(draft: Values, loaded: Values, eq?: Equal): string[]`, `settle(draft: Values, sent: Values, eq?: Equal): Values`, `rejected(message: string): { lines: string[]; more: number }` and `unsavedText(n: number): string`.
- `web/src/components/save-bar.tsx` exports `SaveBar({ count, saving, disabled?, onSave, onDiscard })` and `RejectedAlert({ message }: { message: string })`.
- `web/src/components/unsaved-guard.tsx` exports `UnsavedGuard({ count, page, saving, onSave, onDiscard }: { count: number; page: string; saving: boolean; onSave: () => Promise<boolean>; onDiscard: () => void })`.

**C9 routes (Task 8):** `web/src/router.tsx` gains `/repositories/$name` under the `app` layout route, rendering `RepositoryPage` from `web/src/pages/repository.tsx`. Task 18 gives `/login` the search `{ redirect?: string }`.

**C10 page modules:** `pages/repositories.tsx` `RepositoriesPage` (Task 6); `components/add-repo-dialog.tsx` `AddRepoDialog({ open, onClose })` (Task 7); `pages/repository.tsx` `RepositoryPage` (Task 8), which renders `LabelCheckCard` (Task 9, `components/label-check-card.tsx`) and `RegistrationsCard` (Task 10, `components/registrations-card.tsx`); `pages/storage.tsx` `StoragePage` (Task 11); `components/install-dialog.tsx` `InstallDialog({ open, onClose })` (Task 13); `pages/settings.tsx` `SettingsPage` (Task 15), which renders `TokenCard` (Task 16, `components/token-card.tsx`), `MaintenanceCard` and `AccountCard` (Task 17, `components/maintenance-card.tsx`, `components/account-card.tsx`). Shared helpers the later tasks add: `lib/activity.ts` and `components/repo-summary.tsx` (`repoState`, `RepoSummary`, `ActivitySummary`, Task 6); `lib/labels.ts` (`OS_ARCH`, `matchLabels`, `classify`, Task 9); `components/refused-hint.tsx` (`busyJobs`, `RefusedHint`, Task 11); `components/toolchains-card.tsx` (`ToolchainsCard`, `lastDotnetMajor`, `queueText`, Task 12); `components/storage-cards.tsx` (`PackageCachesCard`, `OperationsCard`, `useOperationToasts`, Task 14). The exact props of each are in the task that creates it.

## Assumptions (evidence)

- Every route part 3 calls already exists, with these codes: `PATCH /config`, `POST /repos`, `DELETE /repos/{name}`, pause/resume, `DELETE …/registrations/{id}` and `DELETE /runner-update` answer 204. `POST …/label-check`, `POST /prune`, `POST /runner-update` and the storage writes answer 202. `POST /reload` answers 200 with a JSON array. `PUT /token` reads the raw body, trimmed (`internal/api/server.go:133-247`, read 2026-10-06).
- `model.RepoPatch` is `{max?, warm?, labels?, cleanup_name_prefixes?, paused?}` and `model.RunnerLimitsPatch` is `{memory_max?, cpu_quota?}` (`internal/model/model.go:113-124`).
- `config.ParseDuration` accepts `Nd` (whole days, optional sign) or Go's `time.ParseDuration` syntax, and `Duration.String` prints whole days as `Nd` (`internal/config/config.go:86-107`). `GET /config` therefore serves durations such as `10s`, `2m0s` and `30d`.
- The TUI behaviour mirrored here (texts, fields, validations, flows) comes from `internal/tui/{repos,manage,dialogs,settings,configpage,storage,install}.go`, read 2026-10-06. Each task quotes the strings it needs.
- darkraise-ui 6.9.6 exports used beyond part 2's: `darkraise-ui/components/dialog` (`Dialog` with `open`/`onOpenChange`, `DialogContent`, `DialogHeader`, `DialogBody`, `DialogFooter`, `DialogTitle`, `DialogDescription`), read from `web/node_modules/darkraise-ui/dist/components/dialog/Dialog.d.ts` 2026-10-06. Unverified that `DialogContent` renders role `dialog` in jsdom — Task 7's test verifies it.
- `useBlocker({ shouldBlockFn, enableBeforeUnload, withResolver: true })` in @tanstack/react-router 1.170 returns `{ status, proceed, reset }` (`node_modules/@tanstack/react-router/dist/esm/useBlocker.d.ts:34-44`). Unverified that `router.history.push` on a memory history consults the blocker — Task 5's test verifies it.
- Part 2's tests use TUI-free conventions this plan keeps: `renderApp` renders through `App`, which holds the `ThemeProvider` that `SidebarLayout` needs, and `authedRoutes` answers the four shell polls (part 2 plan, C11 and Assumption 92).
- Per-repository editing goes on its own route, `/repositories/$name`, not in a side panel as in the TUI. The spec asks for "a card per repo" with the actions listed; the route keeps each page file focused, and the repository's edit form, label check and registrations sit together, as the label check's quick-add needs. Like `/runners/$id`, it does not highlight the sidebar's Repositories item (`activeExact`, accepted in part 2).
- Replace token confirms before sending: spec §5 lists it among the confirmed actions, although the TUI does not confirm it. The spec wins.
- Repository texts and rules ported from the TUI: the label check uses the daemon's `sched.MatchLabels` (every job label among the effective labels, case-insensitive, `internal/sched/sched.go:50-61`) and the TUI's `classify`, including its case-sensitive `missing` test and the `self-hosted` check (`internal/tui/manage.go:719-747`); OS/arch labels from `manage.go:591`; quick-add offers each label once across groups (`manage.go:769-787`). Runners get `config.SystemLabels` (`self-hosted linux x64`, `internal/config/config.go:35`) then the custom labels: global, then repo, trimmed, lower-cased, de-duplicated (`config.go:259-276`). Activity uses 7 days or the shorter history retention, counts the success rate over known conclusions only, marks `≥` when the read hit 500 entries, and draws the newest 20 oldest first (`manage.go:946-1029`); the repo summary follows `internal/tui/repos.go:404-423`. Registration delete errors use the global error toast (bare error text) rather than the TUI's `delete failed: {err}`.
- Storage texts and rules ported from the TUI: the popular-set confirmation is fixed text built from `toolchain.Popular` (`internal/toolchain/set.go:17-23`, as `internal/tui/storage.go:812-826` builds it); the last-.NET-SDK rule (`storage.go:754-792`); the prune questions and the `{reclaim}` suffix (`:550-593`); the last-prune line, timed by `finished_at` (`:465-498`); the finished-operation toast (`:113-146`); busy hints only while some runner is busy (`:390-403`). The Install button is disabled while the target is empty, as in the TUI, so `pick or type a version first` cannot appear and is not shown. Storage tests assume vitest's `TZ=UTC`.
- Settings texts and rules: `GET /config` serves durations in Go's format (`start_timeout` `2m0s` in `web/src/api/fixtures/config.json`), so `120s` must compare equal to `2m0s`; the number checks reuse the daemon's messages `global_max must be >= 1` and `disk_high_water must be 1..100` (`internal/config/config.go:279-370`). The `daemon did not apply …` toast names limit fields as `runner_limits.memory_max` / `runner_limits.cpu_quota`. A token-read error shows its retry time with `errorText`'s `— retry after HH:MM`, not the TUI's `, try again after HH:MM`. New texts with no TUI equivalent: the token confirmation `Replace the GitHub token? ghr uses the new one at once.` (action `Replace`), the token field label `Token`, and the Account card's `Account`, `Current password`, `New password`, `Confirm new password`, `Change password`, `the current password is wrong`, `password changed`, `Log out` (length and match errors reuse the login page's strings).
- darkraise-ui facts read from the installed `.d.ts` files 2026-10-06: `Select` takes `disabled` (`select/Select.d.ts:16`), `Input` takes `suffix` (`input/Input.d.ts:8`), `Dialog` takes an optional `role` (`dialog/Dialog.d.ts:11`), `Progress` takes HTML attributes (`progress/Progress.d.ts`). Unverified that an `AlertDialog` opened over an open `Dialog` is clickable in jsdom — Task 16's test verifies it and its Step 4 gives the fallback.
- tsc cannot catch a renamed optional Go field: an imported JSON value gets no excess-property check and a missing optional key is allowed (`web/src/api/types.test.ts:33-48`), so Task 21 asserts the degraded fixture's optional fields at runtime. Only the CI `test` job carries the `no inlined fonts` guard; the release job checks `test -f dist/index.html` (`.github/workflows/ci.yml:35-37,81`).
- The acceptance build mirrors the release job: `npm ci && npm run build`, then `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "-s -w -X main.version=…"` (`ci.yml:78-90`); `setup.sh` installs with `install -m 0755 … /usr/local/bin/ghr` (homelab `github-runner/setup.sh:139`).
- Unverified that TanStack treats `/login`'s all-optional search as optional in `navigate({ to: "/login" })` — Task 18's typecheck verifies it (fallback `search: {}`). Unverified that the sidebar renders one link named `History` — Task 22 verifies it (fallback: the first match). Unverified that homelab `github-runner/tests/setup_test.sh` runs on Windows — Task 23 falls back to reviewing the diff.
- `pages/dashboard.tsx` keeps its own `repoState`, the same priority rule as Task 6's shared one; this plan does not touch the Dashboard's repo table (Task 20 edits only its glyphs).
- File-shape counting for the Evaluation lines: a test file and the test helpers it introduces count as one shape; a generated file (lockfile, fixtures) counts with the file that generates it.
- Register `docs/superpowers/registers/2026-10-05-ghr-web-ui.md`: rows 11-19 are this plan's. Rows 2, 5 and 7 stay `verify` and close on Task 24's checks. Row 20 stays deferred.

## Task index

1. web: API client for the remaining routes
2. web: hooks for the new pages
3. web: confirm dialog and tag field
4. web: duration parsing and checks
5. web: drafts, save bar and unsaved guard
6. web: Repositories page
7. web: Add repository dialog
8. web: repository settings page
9. web: workflow labels card
10. web: GitHub registrations card
11. web: Storage page and Docker disk
12. web: Toolchains card
13. web: Install toolchain dialog
14. web: package caches and recent operations
15. web: Settings form
16. web: GitHub token card
17. web: maintenance and account cards
18. web: login redirect and 401 guard
19. web: Stop dialog and row button names
20. web: log follow, glyph labels and job icons
21. Go: degraded fixture and CI font guard
22. web: test timing, cleanup and navigation retry
23. homelab: web UI docs
24. Manual acceptance on the runner LXC

---

### Task 1: web: API client for the remaining routes

**Files:**
- Modify: `web/src/api/types.ts` (append), `web/src/api/client.ts`, `web/src/test/api.ts`, `web/src/test/fixtures.ts`
- Test: `web/src/api/client-pages.test.ts`

**Interfaces:**
- Consumes: C1.
- Produces: C2, C3, C4.

**Items:** 11, 12, 13
**Implementer:** dr-superpowers:impl-sonnet-high
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 2 = 5

- [ ] **Step 1: Create the worktree and install**

```bash
cd D:/Repositories/Personal/ghr && timeout 60 git worktree add ../ghr-web-pages -b feat/web-pages be0033b
cd D:/Repositories/Personal/ghr-web-pages && timeout 600 npm --prefix web ci
timeout 600 npm --prefix web test
```
Expected: the worktree exists on `feat/web-pages`, and the part 2 suite passes.

- [ ] **Step 2: Append the patch and request types to `web/src/api/types.ts`**

```ts
export interface RunnerLimitsPatch {
  memory_max?: string
  cpu_quota?: string
}

export interface RepoPatch {
  max?: number
  warm?: number
  labels?: string[]
  cleanup_name_prefixes?: string[]
  paused?: boolean
}

export interface ConfigPatch {
  mode?: string
  global_max?: number
  poll_interval?: string
  start_timeout?: string
  idle_timeout?: string
  history_retention?: string
  disk_high_water?: number
  build_cache_keep?: string
  labels?: string[]
  runner_limits?: RunnerLimitsPatch
  repos?: Record<string, RepoPatch>
}

export interface AddRepoRequest {
  name: string
  max?: number
  labels?: string[]
  allow_public: boolean
}

export type PruneScope = "standard" | "build-cache-keep" | "build-cache-all" | "dangling-images" | "unused-volumes"
```

- [ ] **Step 3: Teach the test helpers raw bodies, content types and pending handlers**

Replace `web/src/test/api.ts` with:

```ts
import { vi } from "vitest"

export interface Call {
  method: string
  path: string
  search: string
  body: unknown
  contentType: string | undefined
}

type Handler = (req: { url: URL; method: string; body: unknown }) => unknown

export function json(value: unknown, status = 200): Response {
  return new Response(JSON.stringify(value), { status, headers: { "Content-Type": "application/json" } })
}

export function noContent(): Response {
  return new Response(null, { status: 204 })
}

function parseBody(body: BodyInit | null | undefined): unknown {
  if (typeof body !== "string") return undefined
  try {
    return JSON.parse(body) as unknown
  } catch {
    return body
  }
}

// Routes are keyed "<METHOD> <path>". A value is a JSON body, or a handler
// returning (or resolving to) a JSON body or a Response; a Response body can
// be read once, so a route that answers with a Response must be a handler.
export function mockApi(routes: Record<string, unknown>) {
  const calls: Call[] = []
  const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit): Promise<Response> => {
    const raw = typeof input === "string" ? input : input instanceof URL ? input.href : input.url
    const url = new URL(raw, "http://localhost")
    const method = (init?.method ?? "GET").toUpperCase()
    const body = parseBody(init?.body)
    const contentType = (init?.headers as Record<string, string> | undefined)?.["Content-Type"]
    calls.push({ method, path: url.pathname, search: url.search, body, contentType })
    const route = routes[`${method} ${url.pathname}`]
    if (route === undefined) return json({ error: `no mock for ${method} ${url.pathname}` }, 404)
    const out = typeof route === "function" ? await (route as Handler)({ url, method, body }) : route
    return out instanceof Response ? out : json(out)
  })
  vi.stubGlobal("fetch", fetchMock)
  return { calls, fetchMock }
}
```

Replace `web/src/test/fixtures.ts` with:

```ts
import availableReposJson from "@/api/fixtures/available-repos.json"
import configJson from "@/api/fixtures/config.json"
import containersJson from "@/api/fixtures/containers.json"
import eventsJson from "@/api/fixtures/events.json"
import historyJson from "@/api/fixtures/history.json"
import labelCheckJson from "@/api/fixtures/label-check.json"
import logJson from "@/api/fixtures/log.json"
import metricsJson from "@/api/fixtures/metrics.json"
import registrationsJson from "@/api/fixtures/registrations.json"
import statusJson from "@/api/fixtures/status.json"
import stepsJson from "@/api/fixtures/steps.json"
import storageJson from "@/api/fixtures/storage.json"
import tokenJson from "@/api/fixtures/token.json"
import choicesJson from "@/api/fixtures/toolchain-choices.json"
import type {
  AvailableRepo,
  Config,
  Container,
  GhrEvent,
  HistoryEntry,
  LabelCheck,
  LogChunk,
  Metrics,
  Registration,
  Status,
  Step,
  Storage,
  TokenStatus,
  ToolchainChoice,
} from "@/api/types"

const status: Status = statusJson
const events: GhrEvent[] = eventsJson
const history: HistoryEntry[] = historyJson
const log: LogChunk = logJson
const steps: Step[] = stepsJson
const containers: Container[] = containersJson
const metrics: Metrics = metricsJson
const config: Config = configJson
const storage: Storage = storageJson
const token: TokenStatus = tokenJson
const labelCheck: LabelCheck = labelCheckJson
const registrations: Registration[] = registrationsJson
const availableRepos: AvailableRepo[] = availableReposJson
const toolchainChoices: ToolchainChoice[] = choicesJson

export const fixtures = {
  status,
  events,
  history,
  log,
  steps,
  containers,
  metrics,
  config,
  storage,
  token,
  labelCheck,
  registrations,
  availableRepos,
  toolchainChoices,
}

export function authedRoutes(over: Record<string, unknown> = {}): Record<string, unknown> {
  return {
    "GET /auth/state": { setup_required: false, authenticated: true },
    "GET /api/status": fixtures.status,
    "GET /api/config": fixtures.config,
    "GET /api/metrics": fixtures.metrics,
    "GET /api/events": fixtures.events,
    ...over,
  }
}
```

- [ ] **Step 4: Write the failing client test**

Create `web/src/api/client-pages.test.ts`:

```ts
import { describe, expect, it } from "vitest"
import { mockApi, noContent } from "@/test/api"
import { api } from "./client"

const ok = () => noContent()

describe("api for the later pages", () => {
  it.each([
    ["patchConfig", () => api.patchConfig({ global_max: 3, runner_limits: { cpu_quota: "200%" } }), "PATCH", "/api/config", { global_max: 3, runner_limits: { cpu_quota: "200%" } }],
    ["addRepo", () => api.addRepo({ name: "new-repo", allow_public: false }), "POST", "/api/repos", { name: "new-repo", allow_public: false }],
    ["removeRepo", () => api.removeRepo("dark mem"), "DELETE", "/api/repos/dark%20mem", undefined],
    ["pauseRepo", () => api.pauseRepo("darkmem"), "POST", "/api/repos/darkmem/pause", undefined],
    ["resumeRepo", () => api.resumeRepo("darkmem"), "POST", "/api/repos/darkmem/resume", undefined],
    ["startLabelCheck", () => api.startLabelCheck("darkmem"), "POST", "/api/repos/darkmem/label-check", undefined],
    ["deleteRegistration", () => api.deleteRegistration("darkmem", 7), "DELETE", "/api/repos/darkmem/registrations/7", undefined],
    ["queueRunnerUpdate", () => api.queueRunnerUpdate(), "POST", "/api/runner-update", undefined],
    ["cancelRunnerUpdate", () => api.cancelRunnerUpdate(), "DELETE", "/api/runner-update", undefined],
    ["refreshStorage", () => api.refreshStorage(), "POST", "/api/storage/refresh", undefined],
    ["installToolchain", () => api.installToolchain("node", "24"), "POST", "/api/toolchains", { tool: "node", version: "24" }],
    ["installPreset", () => api.installPreset("popular"), "POST", "/api/toolchains", { preset: "popular" }],
    ["removeToolchain", () => api.removeToolchain("node", "22.11.0"), "DELETE", "/api/toolchains/node/22.11.0", undefined],
    ["clearCache", () => api.clearCache("nuget"), "POST", "/api/caches/nuget/clear", undefined],
    ["prune standard", () => api.prune("standard"), "POST", "/api/prune", undefined],
    ["prune a scope", () => api.prune("unused-volumes"), "POST", "/api/prune/unused-volumes", undefined],
  ])("%s sends its request", async (_name, call, method, path, body) => {
    const { calls } = mockApi({ [`${method} ${path}`]: ok })
    await call()
    expect(calls).toHaveLength(1)
    expect(calls[0]).toMatchObject({ method, path, body })
  })

  it("sends a replacement token as plain text, unchanged", async () => {
    const { calls } = mockApi({ "PUT /api/token": ok })
    await api.replaceToken("github_pat_abc")
    expect(calls[0]).toMatchObject({ method: "PUT", path: "/api/token", body: "github_pat_abc", contentType: "text/plain" })
  })

  it("reads the reload warnings", async () => {
    mockApi({ "POST /api/reload": ["web settings changed; restart ghr to apply"] })
    expect(await api.reload()).toEqual(["web settings changed; restart ghr to apply"])
  })

  it("reads the per-repo and storage endpoints", async () => {
    const { calls } = mockApi({
      "GET /api/repos/available": [],
      "GET /api/repos/darkmem/label-check": { state: "not_checked", partial: false, groups: [] },
      "GET /api/repos/darkmem/registrations": [],
      "GET /api/token": { state: "ok" },
      "GET /api/storage": { measuring: false },
      "GET /api/toolchains/available": [],
    })
    await api.availableRepos()
    await api.labelCheck("darkmem")
    await api.registrations("darkmem")
    await api.token()
    await api.storage()
    await api.toolchainChoices("node")
    expect(calls.map((c) => `${c.path}${c.search}`)).toEqual([
      "/api/repos/available",
      "/api/repos/darkmem/label-check",
      "/api/repos/darkmem/registrations",
      "/api/token",
      "/api/storage",
      "/api/toolchains/available?tool=node",
    ])
  })
})
```

- [ ] **Step 5: Run it to see it fail**

Run: `timeout 300 npm --prefix web test -- src/api/client-pages.test.ts`
Expected: FAIL, `api.patchConfig is not a function` (and the like).

- [ ] **Step 6: Implement the client**

In `web/src/api/client.ts`, replace the first line with:

```ts
import type {
  AddRepoRequest,
  AuthState,
  AvailableRepo,
  Config,
  ConfigPatch,
  Container,
  GhrEvent,
  HistoryEntry,
  LabelCheck,
  LogChunk,
  Metrics,
  PruneScope,
  Registration,
  Status,
  Step,
  Storage,
  TokenStatus,
  ToolchainChoice,
} from "./types"
```

Replace the start of `request`, from its signature through the `fetch` call, with:

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
```

The rest of `request` (from `if (res.ok) {`) stays as it is. Append these members inside the `api` object, after `resumeAll`:

```ts
  patchConfig: (patch: ConfigPatch) => send("PATCH", "/api/config", patch),
  reload: () => request<string[]>("POST", "/api/reload"),
  availableRepos: (signal?: AbortSignal) => request<AvailableRepo[]>("GET", "/api/repos/available", undefined, signal),
  addRepo: (req: AddRepoRequest) => send("POST", "/api/repos", req),
  removeRepo: (name: string) => send("DELETE", `/api/repos/${seg(name)}`),
  pauseRepo: (name: string) => send("POST", `/api/repos/${seg(name)}/pause`),
  resumeRepo: (name: string) => send("POST", `/api/repos/${seg(name)}/resume`),
  labelCheck: (name: string, signal?: AbortSignal) =>
    request<LabelCheck>("GET", `/api/repos/${seg(name)}/label-check`, undefined, signal),
  startLabelCheck: (name: string) => send("POST", `/api/repos/${seg(name)}/label-check`),
  registrations: (name: string, signal?: AbortSignal) =>
    request<Registration[]>("GET", `/api/repos/${seg(name)}/registrations`, undefined, signal),
  deleteRegistration: (name: string, id: number) => send("DELETE", `/api/repos/${seg(name)}/registrations/${id}`),
  token: (signal?: AbortSignal) => request<TokenStatus>("GET", "/api/token", undefined, signal),
  replaceToken: (token: string) => send("PUT", "/api/token", token),
  queueRunnerUpdate: () => send("POST", "/api/runner-update"),
  cancelRunnerUpdate: () => send("DELETE", "/api/runner-update"),
  storage: (signal?: AbortSignal) => request<Storage>("GET", "/api/storage", undefined, signal),
  refreshStorage: () => send("POST", "/api/storage/refresh"),
  toolchainChoices: (tool: string, signal?: AbortSignal) =>
    request<ToolchainChoice[]>("GET", `/api/toolchains/available?${query({ tool })}`, undefined, signal),
  installToolchain: (tool: string, version: string) => send("POST", "/api/toolchains", { tool, version }),
  installPreset: (preset: string) => send("POST", "/api/toolchains", { preset }),
  removeToolchain: (tool: string, version: string) => send("DELETE", `/api/toolchains/${seg(tool)}/${seg(version)}`),
  clearCache: (name: string) => send("POST", `/api/caches/${seg(name)}/clear`),
  prune: (scope: PruneScope) => send("POST", scope === "standard" ? "/api/prune" : `/api/prune/${seg(scope)}`),
```

- [ ] **Step 7: Run the checks**

Run: `timeout 300 npm --prefix web test -- src/api/client-pages.test.ts`, then the four web checks from Global Constraints.
Expected: all PASS; lint shows no errors.

- [ ] **Step 8: Commit**

```bash
git add web/src/api/types.ts web/src/api/client.ts web/src/api/client-pages.test.ts web/src/test/api.ts web/src/test/fixtures.ts
git commit -m "feat(web): add the API calls the later pages use"
```

### Task 2: web: hooks for the new pages

**Files:**
- Modify: `web/src/api/hooks.ts`
- Test: `web/src/api/hooks-pages.test.tsx`

**Interfaces:**
- Consumes: C3, C4.
- Produces: C5.

**Items:** 11, 12, 13
**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 0 = 3

- [ ] **Step 1: Write the failing test**

Create `web/src/api/hooks-pages.test.tsx`:

```tsx
import { renderHook, waitFor } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import type { LabelCheck, Status, Storage } from "@/api/types"
import { mockApi } from "@/test/api"
import { fixtures } from "@/test/fixtures"
import { withQuery } from "@/test/query"
import { storageBusy, useLabelCheck, useRepoActivity } from "./hooks"

const idle: Storage = {
  ...fixtures.storage,
  measuring: false,
  operations: { current: null, queued: 0, recent: [] },
}
const calm: Status = { ...fixtures.status, maintenance: { running: false } }

describe("storageBusy", () => {
  it("is false when nothing runs", () => {
    expect(storageBusy(idle, calm)).toBe(false)
    expect(storageBusy(undefined, undefined)).toBe(false)
  })

  it.each([
    ["an operation runs", { ...idle, operations: { ...idle.operations, current: fixtures.storage.operations.current } }, calm],
    ["an operation is queued", { ...idle, operations: { ...idle.operations, queued: 2 } }, calm],
    ["a measurement runs", { ...idle, measuring: true }, calm],
    ["a prune runs", idle, { ...calm, maintenance: { running: true } }],
  ])("is true when %s", (_why, storage, status) => {
    expect(storageBusy(storage, status)).toBe(true)
  })
})

describe("useLabelCheck", () => {
  it(
    "polls while the check runs and stops when it is done",
    async () => {
      let reads = 0
      const { calls } = mockApi({
        "GET /api/repos/darkmem/label-check": (): LabelCheck => {
          reads++
          return reads < 3 ? { state: "checking", partial: false, groups: [] } : fixtures.labelCheck
        },
      })
      const { wrapper } = withQuery()
      const { result } = renderHook(() => useLabelCheck("darkmem", true), { wrapper })
      await waitFor(() => expect(result.current.data?.state).toBe("done"), { timeout: 6000 })
      const settled = calls.length
      await new Promise((r) => setTimeout(r, 1500))
      expect(calls.length).toBe(settled)
    },
    { timeout: 10_000 },
  )

  it("does not read while disabled", async () => {
    const { calls } = mockApi({ "GET /api/repos/darkmem/label-check": fixtures.labelCheck })
    const { wrapper } = withQuery()
    renderHook(() => useLabelCheck("darkmem", false), { wrapper })
    await new Promise((r) => setTimeout(r, 300))
    expect(calls).toHaveLength(0)
  })
})

describe("useRepoActivity", () => {
  it("reads up to 500 history entries for the repo", async () => {
    const { calls } = mockApi({ "GET /api/history": fixtures.history })
    const { wrapper } = withQuery()
    const { result } = renderHook(() => useRepoActivity("darkmem"), { wrapper })
    await waitFor(() => expect(result.current.data).toHaveLength(2))
    expect(calls[0]?.search).toBe("?repo=darkmem&conclusion=&limit=500")
  })
})
```

- [ ] **Step 2: Run it to see it fail**

Run: `timeout 300 npm --prefix web test -- src/api/hooks-pages.test.tsx`
Expected: FAIL, `storageBusy` is not exported.

- [ ] **Step 3: Implement the hooks**

In `web/src/api/hooks.ts`, change the type import to `import type { GhrEvent, LogChunk, Status, Storage } from "./types"`, and add these entries inside `keys`, after `log`:

```ts
  token: ["token"] as const,
  storage: ["storage"] as const,
  availableRepos: ["available-repos"] as const,
  repoActivity: (name: string) => ["repo-activity", name] as const,
  labelCheck: (name: string) => ["label-check", name] as const,
  registrations: (name: string) => ["registrations", name] as const,
  toolchainChoices: (tool: string) => ["toolchain-choices", tool] as const,
```

Append to the file:

```ts
export const ACTIVITY_LIMIT = 500

export function useToken() {
  return useQuery({ queryKey: keys.token, queryFn: ({ signal }) => api.token(signal), refetchInterval: POLL_SLOW })
}

// A prune shows in /status, not /storage; either one running makes the
// Storage page poll every second, as the TUI's storageActive does.
export function storageBusy(storage?: Storage, status?: Status): boolean {
  if (status?.maintenance.running) return true
  if (!storage) return false
  return storage.operations.current !== null || storage.operations.queued > 0 || storage.measuring
}

export function useStorage(status: Status | undefined) {
  return useQuery({
    queryKey: keys.storage,
    queryFn: ({ signal }) => api.storage(signal),
    refetchInterval: (q) => (storageBusy(q.state.data, status) ? POLL_FAST : POLL_SLOW),
  })
}

export function useRepoActivity(name: string) {
  return useQuery({
    queryKey: keys.repoActivity(name),
    queryFn: ({ signal }) => api.history(name, "", ACTIVITY_LIMIT, signal),
    refetchInterval: POLL_SLOW,
  })
}

export function useLabelCheck(name: string, enabled: boolean) {
  return useQuery({
    queryKey: keys.labelCheck(name),
    queryFn: ({ signal }) => api.labelCheck(name, signal),
    enabled,
    refetchInterval: (q) => (enabled && q.state.data?.state === "checking" ? POLL_FAST : false),
  })
}

export function useRegistrations(name: string, enabled: boolean) {
  return useQuery({
    queryKey: keys.registrations(name),
    queryFn: ({ signal }) => api.registrations(name, signal),
    enabled,
  })
}

export function useAvailableRepos(enabled: boolean) {
  return useQuery({ queryKey: keys.availableRepos, queryFn: ({ signal }) => api.availableRepos(signal), enabled, staleTime: 0 })
}

export function useToolchainChoices(tool: string, enabled: boolean) {
  return useQuery({
    queryKey: keys.toolchainChoices(tool),
    queryFn: ({ signal }) => api.toolchainChoices(tool, signal),
    enabled: enabled && tool !== "",
  })
}
```

- [ ] **Step 4: Run the checks**

Run: `timeout 300 npm --prefix web test -- src/api/hooks-pages.test.tsx`, then the four web checks.
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add web/src/api/hooks.ts web/src/api/hooks-pages.test.tsx
git commit -m "feat(web): add hooks for storage, token and repos"
```

### Task 3: web: confirm dialog and tag field

**Files:**
- Create: `web/src/components/confirm-dialog.tsx`, `web/src/components/tag-field.tsx`
- Test: `web/src/components/shared.test.tsx`

**Interfaces:**
- Consumes: nothing.
- Produces: C6 (`ConfirmDialog`, `Confirm`, `TagField`).

**Items:** 14
**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 0 = 3

- [ ] **Step 1: Write the failing test**

Create `web/src/components/shared.test.tsx`:

```tsx
import { render, screen, waitFor } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { useState } from "react"
import { describe, expect, it, vi } from "vitest"
import { ConfirmDialog, type Confirm } from "./confirm-dialog"
import { TagField } from "./tag-field"

function Asker({ run }: { run: () => void }) {
  const [confirm, setConfirm] = useState<Confirm | null>(null)
  return (
    <>
      <button onClick={() => setConfirm({ title: "Remove repo darkmem? Its running jobs finish first.", action: "Remove", destructive: true, run })}>
        ask
      </button>
      <ConfirmDialog confirm={confirm} onClose={() => setConfirm(null)} />
    </>
  )
}

describe("ConfirmDialog", () => {
  it("runs nothing on Cancel", async () => {
    const run = vi.fn()
    const user = userEvent.setup()
    render(<Asker run={run} />)
    await user.click(screen.getByRole("button", { name: "ask" }))
    expect(await screen.findByText("Remove repo darkmem? Its running jobs finish first.")).toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: "Cancel" }))
    await waitFor(() => expect(screen.queryByRole("alertdialog")).toBeNull())
    expect(run).not.toHaveBeenCalled()
  })

  it("runs the action once, closes, and never shows a blank title", async () => {
    const run = vi.fn()
    const user = userEvent.setup()
    render(<Asker run={run} />)
    await user.click(screen.getByRole("button", { name: "ask" }))
    await user.click(await screen.findByRole("button", { name: "Remove" }))
    expect(run).toHaveBeenCalledTimes(1)
    expect(screen.queryByText(/undefined/)).toBeNull()
    await waitFor(() => expect(screen.queryByRole("alertdialog")).toBeNull())
  })
})

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

  it("removes a tag", async () => {
    const user = userEvent.setup()
    render(<Tags initial={["gpu", "big"]} />)
    await user.click(screen.getByRole("button", { name: "Remove gpu" }))
    expect(screen.queryByText("gpu")).toBeNull()
    expect(screen.getByText("big")).toBeInTheDocument()
  })
})
```

- [ ] **Step 2: Run it to see it fail**

Run: `timeout 300 npm --prefix web test -- src/components/shared.test.tsx`
Expected: FAIL, cannot resolve `./confirm-dialog`.

- [ ] **Step 3: Implement the two components**

Create `web/src/components/confirm-dialog.tsx`:

```tsx
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
import { useState } from "react"

export interface Confirm {
  title: string
  body?: string
  action: string
  destructive?: boolean
  run: () => void
}

export function ConfirmDialog({ confirm, onClose }: { confirm: Confirm | null; onClose: () => void }) {
  // The dialog animates closed after the caller drops its request; holding the
  // last one keeps the title from going blank in that moment.
  const [held, setHeld] = useState<Confirm | null>(confirm)
  if (confirm !== null && confirm !== held) setHeld(confirm)
  return (
    <AlertDialog
      open={confirm !== null}
      onOpenChange={(open) => {
        if (!open) onClose()
      }}
    >
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{held?.title}</AlertDialogTitle>
          {held?.body && <AlertDialogDescription>{held.body}</AlertDialogDescription>}
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>Cancel</AlertDialogCancel>
          <AlertDialogAction data-variant={held?.destructive ? "destructive" : "default"} onClick={() => held?.run()}>
            {held?.action}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}
```

Create `web/src/components/tag-field.tsx`:

```tsx
import { Badge } from "darkraise-ui/components/badge"
import { Button } from "darkraise-ui/components/button"
import { Input } from "darkraise-ui/components/input"
import { useState } from "react"

const MAX_TAG = 100

export function TagField({
  label,
  value,
  onChange,
  disabled,
}: {
  label: string
  value: string[]
  onChange: (next: string[]) => void
  disabled?: boolean
}) {
  const [text, setText] = useState("")

  function add() {
    const tag = text.trim()
    if (tag !== "" && tag.length <= MAX_TAG && !value.includes(tag)) onChange([...value, tag])
    setText("")
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
          onChange={(e) => setText(e.target.value)}
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
    </div>
  )
}
```

- [ ] **Step 4: Run the checks**

Run: `timeout 300 npm --prefix web test -- src/components/shared.test.tsx`, then the four web checks.
Expected: all PASS. If lint reports an error for the `setHeld` call during render, keep the pattern (React documents it for "storing information from previous renders") and only change it if the rule is an error, not a warning: then move the hold into a `useRef` updated in a `useEffect` and render from `confirm ?? ref.current`.

- [ ] **Step 5: Commit**

```bash
git add web/src/components/confirm-dialog.tsx web/src/components/tag-field.tsx web/src/components/shared.test.tsx
git commit -m "feat(web): add confirm dialog and tag field"
```

### Task 4: web: duration parsing and checks

**Files:**
- Create: `web/src/lib/duration.ts`
- Test: `web/src/lib/duration.test.ts`

**Interfaces:**
- Consumes: nothing.
- Produces: C7.

**Items:** 13
**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 0 = 3

- [ ] **Step 1: Write the failing test**

Create `web/src/lib/duration.test.ts`:

```ts
import { describe, expect, it } from "vitest"
import { DAY_MS, durationError, parseDuration, sameDuration } from "./duration"

describe("parseDuration", () => {
  it.each([
    ["10s", 10_000],
    ["2m0s", 120_000],
    ["1h30m", 5_400_000],
    ["1.5h", 5_400_000],
    ["250ms", 250],
    ["30d", 30 * DAY_MS],
    ["0", 0],
    ["-5s", -5000],
  ])("reads %s", (text, ms) => {
    expect(parseDuration(text)).toBe(ms)
  })

  it.each(["", "5", "abc", "1d12h", "1.5d", "5 s", "s"])("rejects %j", (text) => {
    expect(parseDuration(text)).toBeNull()
  })
})

describe("durationError", () => {
  it("passes a valid duration", () => {
    expect(durationError("poll_interval", " 15s ", 5000)).toBe("")
  })

  it("names what is wrong, as the TUI does", () => {
    expect(durationError("poll_interval", "soon", 5000)).toBe('invalid duration "soon"')
    expect(durationError("start_timeout", "0s", 0)).toBe("start_timeout must be greater than 0")
    expect(durationError("poll_interval", "2s", 5000)).toBe("poll_interval must be at least 5s")
    expect(durationError("history_retention", "12h", DAY_MS)).toBe("history_retention must be at least 1d")
  })
})

describe("sameDuration", () => {
  it("compares by value, falling back to the text", () => {
    expect(sameDuration("120s", "2m0s")).toBe(true)
    expect(sameDuration("30d", "720h")).toBe(true)
    expect(sameDuration("10s", "11s")).toBe(false)
    expect(sameDuration("bogus", "bogus")).toBe(true)
  })
})
```

- [ ] **Step 2: Run it to see it fail**

Run: `timeout 300 npm --prefix web test -- src/lib/duration.test.ts`
Expected: FAIL, cannot resolve `./duration`.

- [ ] **Step 3: Implement**

Create `web/src/lib/duration.ts`:

```ts
export const DAY_MS = 86_400_000

const UNIT_MS: Record<string, number> = { ns: 1e-6, us: 1e-3, "µs": 1e-3, "μs": 1e-3, ms: 1, s: 1000, m: 60_000, h: 3_600_000 }
const PART = /^(\d+\.?\d*|\.\d+)(ns|us|µs|μs|ms|s|m|h)/

// Mirrors config.ParseDuration: a whole number of days ("30d"), or Go's
// time.ParseDuration syntax.
export function parseDuration(text: string): number | null {
  if (text.endsWith("d")) {
    const days = text.slice(0, -1)
    return /^[+-]?\d+$/.test(days) ? Number(days) * DAY_MS : null
  }
  let rest = text
  let sign = 1
  if (rest.startsWith("-") || rest.startsWith("+")) {
    if (rest.startsWith("-")) sign = -1
    rest = rest.slice(1)
  }
  if (rest === "0") return 0
  if (rest === "") return null
  let total = 0
  while (rest !== "") {
    const m = PART.exec(rest)
    if (!m) return null
    total += Number(m[1]) * (UNIT_MS[m[2] ?? ""] ?? 0)
    rest = rest.slice(m[0].length)
  }
  return sign * total
}

function floorText(ms: number): string {
  return ms % DAY_MS === 0 ? `${ms / DAY_MS}d` : `${ms / 1000}s`
}

export function durationError(name: string, text: string, floorMs: number): string {
  const s = text.trim()
  const v = parseDuration(s)
  if (v === null) return `invalid duration "${s}"`
  if (v <= 0) return `${name} must be greater than 0`
  if (v < floorMs) return `${name} must be at least ${floorText(floorMs)}`
  return ""
}

export function sameDuration(a: string, b: string): boolean {
  const x = parseDuration(a.trim())
  const y = parseDuration(b.trim())
  return x !== null && y !== null ? x === y : a.trim() === b.trim()
}
```

- [ ] **Step 4: Run the checks**

Run: `timeout 300 npm --prefix web test -- src/lib/duration.test.ts`, then the four web checks.
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add web/src/lib/duration.ts web/src/lib/duration.test.ts
git commit -m "feat(web): parse and check config durations"
```

### Task 5: web: drafts, save bar and unsaved guard

**Files:**
- Create: `web/src/lib/draft.ts`, `web/src/components/save-bar.tsx`, `web/src/components/unsaved-guard.tsx`
- Test: `web/src/lib/draft.test.ts`, `web/src/components/unsaved-guard.test.tsx`

**Interfaces:**
- Consumes: nothing.
- Produces: C8.

**Items:** 11, 13
**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 1 = 4

- [ ] **Step 1: Write the failing tests**

Create `web/src/lib/draft.test.ts`:

```ts
import { describe, expect, it } from "vitest"
import { changedKeys, rejected, sameValue, settle, unsavedText, type Equal } from "./draft"

describe("sameValue", () => {
  it("compares lists by element and everything else by identity", () => {
    expect(sameValue(["a", "b"], ["a", "b"])).toBe(true)
    expect(sameValue(["a", "b"], ["b", "a"])).toBe(false)
    expect(sameValue(3, 3)).toBe(true)
    expect(sameValue("3", 3)).toBe(false)
  })
})

describe("changedKeys", () => {
  it("lists the edits that differ from the loaded values", () => {
    const loaded = { mode: "queue", global_max: 2, labels: ["homelab"] }
    expect(changedKeys({ mode: "queue", global_max: 3, labels: ["homelab"] }, loaded)).toEqual(["global_max"])
  })

  it("uses the given comparison", () => {
    const eq: Equal = (key, a, b) => (key === "poll_interval" ? String(a).trim() === String(b).trim() : a === b)
    expect(changedKeys({ poll_interval: " 10s " }, { poll_interval: "10s" }, eq)).toEqual([])
  })
})

describe("settle", () => {
  it("drops edits that were saved and keeps ones made during the save", () => {
    expect(settle({ a: 1, b: 2, c: 3 }, { a: 1, b: 5 })).toEqual({ b: 2, c: 3 })
  })
})

describe("rejected", () => {
  it("shows three messages and counts the rest", () => {
    expect(rejected("a; b")).toEqual({ lines: ["a", "b"], more: 0 })
    expect(rejected("a; b; c; d; e")).toEqual({ lines: ["a", "b", "c"], more: 2 })
  })
})

describe("unsavedText", () => {
  it("counts the changes", () => {
    expect(unsavedText(1)).toBe("● 1 unsaved change")
    expect(unsavedText(4)).toBe("● 4 unsaved changes")
  })
})
```

Create `web/src/components/unsaved-guard.test.tsx`:

```tsx
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  Outlet,
  RouterProvider,
} from "@tanstack/react-router"
import { act, render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { useState } from "react"
import { describe, expect, it, vi } from "vitest"
import { UnsavedGuard } from "./unsaved-guard"

function setup(saveResult = true) {
  const onSave = vi.fn()
  const onDiscard = vi.fn()
  function Form() {
    const [count, setCount] = useState(1)
    return (
      <>
        <p>form page</p>
        <UnsavedGuard
          count={count}
          page="Settings"
          saving={false}
          onSave={async () => {
            onSave()
            if (saveResult) setCount(0)
            return saveResult
          }}
          onDiscard={() => {
            onDiscard()
            setCount(0)
          }}
        />
      </>
    )
  }
  const root = createRootRoute({ component: () => <Outlet /> })
  const form = createRoute({ getParentRoute: () => root, path: "/", component: Form })
  const other = createRoute({ getParentRoute: () => root, path: "/other", component: () => <p>other page</p> })
  const router = createRouter({ routeTree: root.addChildren([form, other]), history: createMemoryHistory({ initialEntries: ["/"] }) })
  render(<RouterProvider router={router} />)
  return { router, onSave, onDiscard, user: userEvent.setup() }
}

async function leave(router: ReturnType<typeof setup>["router"]) {
  await screen.findByText("form page")
  act(() => router.history.push("/other"))
  expect(await screen.findByText("You have 1 unsaved change on the Settings page.")).toBeInTheDocument()
}

describe("UnsavedGuard", () => {
  it("stays on Stay", async () => {
    const { router, user } = setup()
    await leave(router)
    await user.click(screen.getByRole("button", { name: "Stay" }))
    expect(screen.getByText("form page")).toBeInTheDocument()
    expect(router.state.location.pathname).toBe("/")
  })

  it("drops the edits and leaves on Discard", async () => {
    const { router, user, onDiscard } = setup()
    await leave(router)
    await user.click(screen.getByRole("button", { name: "Discard" }))
    expect(await screen.findByText("other page")).toBeInTheDocument()
    expect(onDiscard).toHaveBeenCalledTimes(1)
  })

  it("saves, then leaves", async () => {
    const { router, user, onSave } = setup()
    await leave(router)
    await user.click(screen.getByRole("button", { name: "Save" }))
    expect(await screen.findByText("other page")).toBeInTheDocument()
    expect(onSave).toHaveBeenCalledTimes(1)
  })

  it("stays when the save fails", async () => {
    const { router, user } = setup(false)
    await leave(router)
    await user.click(screen.getByRole("button", { name: "Save" }))
    expect(await screen.findByText("form page")).toBeInTheDocument()
    expect(router.state.location.pathname).toBe("/")
  })
})
```

- [ ] **Step 2: Run them to see them fail**

Run: `timeout 300 npm --prefix web test -- src/lib/draft.test.ts src/components/unsaved-guard.test.tsx`
Expected: FAIL, cannot resolve `./draft` and `./unsaved-guard`.

- [ ] **Step 3: Implement the draft model**

Create `web/src/lib/draft.ts`:

```ts
export type Values = Record<string, unknown>
export type Equal = (key: string, a: unknown, b: unknown) => boolean

export function sameValue(a: unknown, b: unknown): boolean {
  if (Array.isArray(a) && Array.isArray(b)) return a.length === b.length && a.every((v, i) => v === b[i])
  return a === b
}

export const plainEqual: Equal = (_key, a, b) => sameValue(a, b)

export function changedKeys(draft: Values, loaded: Values, eq: Equal = plainEqual): string[] {
  return Object.keys(draft).filter((k) => !eq(k, draft[k], loaded[k]))
}

// After a save, an edit still equal to what was sent is settled; one made
// while the save was in flight stays pending.
export function settle(draft: Values, sent: Values, eq: Equal = plainEqual): Values {
  return Object.fromEntries(Object.entries(draft).filter(([k, v]) => !(k in sent) || !eq(k, v, sent[k])))
}

export function rejected(message: string): { lines: string[]; more: number } {
  const all = message.split("; ")
  return { lines: all.slice(0, 3), more: Math.max(0, all.length - 3) }
}

export function unsavedText(n: number): string {
  return n === 1 ? "● 1 unsaved change" : `● ${n} unsaved changes`
}
```

- [ ] **Step 4: Implement the save bar and the guard**

Create `web/src/components/save-bar.tsx`:

```tsx
import { Alert, AlertDescription, AlertTitle } from "darkraise-ui/components/alert"
import { Button } from "darkraise-ui/components/button"
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
    <div className="sticky bottom-0 z-10 mt-4 flex items-center justify-between gap-4 rounded-md border bg-background p-3 shadow">
      <span className="text-sm text-amber-600">{unsavedText(count)}</span>
      <div className="flex gap-2">
        <Button variant="secondary" disabled={saving || disabled} onClick={onDiscard}>
          Discard
        </Button>
        <Button disabled={saving || disabled} onClick={onSave}>
          {saving ? "Saving…" : "Save changes"}
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
        <ul>
          {lines.map((line) => (
            <li key={line}>✖ {line}</li>
          ))}
          {more > 0 && <li>… and {more} more</li>}
        </ul>
      </AlertDescription>
    </Alert>
  )
}
```

Create `web/src/components/unsaved-guard.tsx`:

```tsx
import { useBlocker } from "@tanstack/react-router"
import {
  AlertDialog,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "darkraise-ui/components/alert-dialog"
import { Button } from "darkraise-ui/components/button"

export function UnsavedGuard({
  count,
  page,
  saving,
  onSave,
  onDiscard,
}: {
  count: number
  page: string
  saving: boolean
  onSave: () => Promise<boolean>
  onDiscard: () => void
}) {
  const blocker = useBlocker({ shouldBlockFn: () => count > 0 || saving, enableBeforeUnload: count > 0, withResolver: true })
  if (blocker.status !== "blocked") return null
  const { proceed, reset } = blocker
  const changes = count === 1 ? "1 unsaved change" : `${count} unsaved changes`
  return (
    <AlertDialog
      open
      onOpenChange={(open) => {
        if (!open) reset()
      }}
    >
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>Unsaved changes</AlertDialogTitle>
          <AlertDialogDescription>{saving ? "wait for the save to finish" : `You have ${changes} on the ${page} page.`}</AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <Button variant="secondary" onClick={reset}>
            Stay
          </Button>
          <Button
            variant="secondary"
            disabled={saving}
            onClick={() => {
              onDiscard()
              proceed()
            }}
          >
            Discard
          </Button>
          <Button disabled={saving} onClick={() => void onSave().then((ok) => (ok ? proceed() : reset()))}>
            Save
          </Button>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}
```

- [ ] **Step 5: Run the checks**

Run: `timeout 300 npm --prefix web test -- src/lib/draft.test.ts src/components/unsaved-guard.test.tsx`, then the four web checks.
Expected: all PASS. If `router.history.push` does not reach the blocker (the dialog never appears), drive the navigation with `act(() => void router.navigate({ to: "/other" } as never))` instead, and record the finding in the commit body.

- [ ] **Step 6: Commit**

```bash
git add web/src/lib/draft.ts web/src/lib/draft.test.ts web/src/components/save-bar.tsx web/src/components/unsaved-guard.tsx web/src/components/unsaved-guard.test.tsx
git commit -m "feat(web): add drafts, save bar and unsaved guard"
```

### Task 6: web: Repositories page

**Files:**
- Create: `web/src/lib/activity.ts`, `web/src/components/repo-summary.tsx`
- Modify: `web/src/pages/repositories.tsx` (replace the placeholder)
- Test: `web/src/lib/activity.test.ts`, `web/src/pages/repositories.test.tsx`

**Interfaces:**
- Consumes: C1, C3 (`pauseRepo`, `resumeRepo`, `removeRepo`), C5 (`useRepoActivity`, `ACTIVITY_LIMIT`, `keys`), C6 (`ConfirmDialog`), C7 (`parseDuration`, `DAY_MS`).
- Produces: `web/src/lib/activity.ts` exports `interface StripMark { id: string; symbol: string; label: string; className: string }`, `interface Activity { line: string; label: string; strip: StripMark[] }`, `activityWindow(retention: string | undefined): { ms: number; label: string }` and `summarize(entries: HistoryEntry[], now: number, retention: string | undefined, limit: number): Activity`. `web/src/components/repo-summary.tsx` exports `repoState(r: RepoStatus): string`, `RepoSummary({ repo, now }: { repo: RepoStatus; now: number })` and `ActivitySummary({ name, retention, now }: { name: string; retention: string | undefined; now: number })`. Task 8 renders both components; Tasks 7 and 8 edit `RepositoriesPage`.

**Items:** 11, 14
**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 0 = 3

- [ ] **Step 1: Write the failing tests**

Create `web/src/lib/activity.test.ts`:

```ts
import { describe, expect, it } from "vitest"
import type { HistoryEntry } from "@/api/types"
import { activityWindow, summarize } from "./activity"

const NOW = Date.parse("2026-10-06T12:00:00Z")
const H = 3_600_000

function entry(id: string, conclusion: string, hoursAgo: number, minutes: number): HistoryEntry {
  const finished = NOW - hoursAgo * H
  return {
    id,
    repo: "darkmem",
    run_id: 1,
    run_number: id,
    workflow: "ci",
    job_name: "build",
    conclusion,
    started_at: new Date(finished - minutes * 60_000).toISOString(),
    finished_at: new Date(finished).toISOString(),
  }
}

// Newest first, as GET /history serves it.
const entries = [
  entry("5", "success", 1, 5),
  entry("4", "failure", 2, 3),
  entry("3", "cancelled", 3, 1),
  entry("2", "", 4, 1),
  entry("1", "success", 8 * 24, 9),
]

describe("activityWindow", () => {
  it("is 7 days unless the retention is shorter", () => {
    expect(activityWindow(undefined)).toEqual({ ms: 7 * 24 * H, label: "last 7 days" })
    expect(activityWindow("30d")).toEqual({ ms: 7 * 24 * H, label: "last 7 days" })
    expect(activityWindow("3d")).toEqual({ ms: 3 * 24 * H, label: "last 3d" })
  })
})

describe("summarize", () => {
  it("counts the window's jobs, the success rate over known results and the average", () => {
    const a = summarize(entries, NOW, "30d", 500)
    expect(a.line).toBe("4 jobs · 33% success · avg 2m30s")
    expect(a.label).toBe("last 7 days")
  })

  it("draws the newest results oldest first, with a name for each mark", () => {
    const a = summarize(entries, NOW, undefined, 500)
    expect(a.strip.map((m) => m.symbol)).toEqual(["?", "○", "✖", "■"])
    expect(a.strip.at(-1)?.label).toBe("#5 build: success")
    expect(a.strip[0]?.label).toBe("#2 build: unknown")
  })

  it("marks a capped read and an empty window", () => {
    expect(summarize(entries, NOW, undefined, 5).line).toBe("≥4 jobs · 33% success · avg 2m30s")
    expect(summarize([], NOW, undefined, 500)).toEqual({ line: "0 jobs · – success · avg –", label: "last 7 days", strip: [] })
  })
})
```

Create `web/src/pages/repositories.test.tsx`:

```tsx
import { screen, waitFor, within } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import type { HistoryEntry } from "@/api/types"
import { mockApi, noContent } from "@/test/api"
import { authedRoutes, fixtures } from "@/test/fixtures"
import { renderApp } from "@/test/render"

function entry(id: string, conclusion: string, minutesAgo: number): HistoryEntry {
  const finished = Date.now() - minutesAgo * 60_000
  return {
    id,
    repo: "darkmem",
    run_id: Number(id),
    run_number: id,
    workflow: "ci",
    job_name: "build",
    conclusion,
    started_at: new Date(finished - 5 * 60_000).toISOString(),
    finished_at: new Date(finished).toISOString(),
  }
}

const history = [entry("3", "success", 10), entry("2", "failure", 20), entry("1", "cancelled", 30)]

function routes(over: Record<string, unknown> = {}) {
  return authedRoutes({ "GET /api/history": history, ...over })
}

describe("Repositories page", () => {
  it("shows a card per repo with its state, counts and activity", async () => {
    mockApi(routes())
    renderApp("/repositories")
    expect(await screen.findByText("1/2 running · 3 queued")).toBeInTheDocument()
    expect(screen.getByText("darkcloud")).toBeInTheDocument()
    expect(screen.getByText("removing… running jobs finish first")).toBeInTheDocument()
    expect(screen.getByText("GitHub: not found")).toBeInTheDocument()
    expect(screen.getByText(/#41 build · /)).toBeInTheDocument()
    expect((await screen.findAllByText("3 jobs · 33% success · avg 5m00s")).length).toBe(3)
    expect(screen.getAllByRole("img", { name: "#3 build: success" })).toHaveLength(3)
  })

  it("waits for the daemon before the first status", async () => {
    mockApi(routes({ "GET /api/status": () => new Response(JSON.stringify({ error: "connection refused" }), { status: 502 }) }))
    renderApp("/repositories")
    expect(await screen.findByText("waiting for the daemon…")).toBeInTheDocument()
  })

  it(
    "keeps the cards but disables their actions while the daemon is unreachable",
    async () => {
      let reads = 0
      mockApi(routes({ "GET /api/status": () => (++reads === 1 ? fixtures.status : new Response(JSON.stringify({ error: "connection refused" }), { status: 502 })) }))
      renderApp("/repositories")
      expect(await screen.findByText("Reconnecting", {}, { timeout: 4000 })).toBeInTheDocument()
      expect(screen.getByRole("button", { name: "Pause darkmem" })).toBeDisabled()
      expect(screen.getByRole("button", { name: "Remove darkmem" })).toBeDisabled()
    },
    { timeout: 10_000 },
  )

  it("says when no repo is configured", async () => {
    mockApi(routes({ "GET /api/status": { ...fixtures.status, repos: [] } }))
    renderApp("/repositories")
    expect(await screen.findByText("No repositories yet")).toBeInTheDocument()
  })

  it("pauses and resumes a repo", async () => {
    const { calls } = mockApi(
      routes({ "POST /api/repos/darkmem/pause": () => noContent(), "POST /api/repos/darkcloud/resume": () => noContent() }),
    )
    const { user } = renderApp("/repositories")
    await user.click(await screen.findByRole("button", { name: "Pause darkmem" }))
    expect((await screen.findAllByText("paused darkmem")).length).toBeGreaterThan(0)
    await user.click(screen.getByRole("button", { name: "Resume darkcloud" }))
    expect((await screen.findAllByText("resumed darkcloud")).length).toBeGreaterThan(0)
    expect(calls.filter((c) => c.method === "POST").map((c) => c.path)).toEqual([
      "/api/repos/darkmem/pause",
      "/api/repos/darkcloud/resume",
    ])
  })

  it("locks a repo that is being removed", async () => {
    mockApi(routes())
    renderApp("/repositories")
    expect(await screen.findByRole("button", { name: "Resume old-repo" })).toBeDisabled()
    expect(screen.getByRole("button", { name: "Remove old-repo" })).toBeDisabled()
  })

  it("removes a repo only after confirmation", async () => {
    const { calls } = mockApi(routes({ "DELETE /api/repos/darkmem": () => noContent() }))
    const { user } = renderApp("/repositories")
    await user.click(await screen.findByRole("button", { name: "Remove darkmem" }))
    expect(await screen.findByText("Remove repo darkmem? Its running jobs finish first.")).toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: "Cancel" }))
    await waitFor(() => expect(screen.queryByRole("alertdialog")).toBeNull())
    expect(calls.some((c) => c.method === "DELETE")).toBe(false)

    await user.click(screen.getByRole("button", { name: "Remove darkmem" }))
    await user.click(within(await screen.findByRole("alertdialog")).getByRole("button", { name: "Remove" }))
    await waitFor(() => expect(calls.some((c) => c.method === "DELETE" && c.path === "/api/repos/darkmem")).toBe(true))
    expect((await screen.findAllByText("removing darkmem")).length).toBeGreaterThan(0)
  })
})
```

- [ ] **Step 2: Run them to see them fail**

Run: `timeout 300 npm --prefix web test -- src/lib/activity.test.ts src/pages/repositories.test.tsx`
Expected: FAIL; `./activity` cannot be resolved, and the page still shows `Not in the web UI yet`.

- [ ] **Step 3: Implement the activity summary**

Create `web/src/lib/activity.ts`:

```ts
import type { HistoryEntry } from "@/api/types"
import { DAY_MS, parseDuration } from "./duration"
import { dur } from "./format"

const WINDOW_MS = 7 * DAY_MS
const STRIP = 20

export interface StripMark {
  id: string
  symbol: string
  label: string
  className: string
}

export interface Activity {
  line: string
  label: string
  strip: StripMark[]
}

const MARKS: Record<string, [string, string]> = {
  success: ["■", "text-green-600"],
  failure: ["✖", "text-destructive"],
  cancelled: ["○", "text-muted-foreground"],
}

export function activityWindow(retention: string | undefined): { ms: number; label: string } {
  const r = retention === undefined ? null : parseDuration(retention)
  return r !== null && r > 0 && r < WINDOW_MS ? { ms: r, label: `last ${retention}` } : { ms: WINDOW_MS, label: "last 7 days" }
}

// Every conclusion GitHub reports counts as known; only a missing one does not.
export function summarize(entries: HistoryEntry[], now: number, retention: string | undefined, limit: number): Activity {
  const win = activityWindow(retention)
  const recent = entries.filter((e) => Date.parse(e.finished_at) >= now - win.ms)
  let ok = 0
  let known = 0
  let total = 0
  for (const e of recent) {
    total += Date.parse(e.finished_at) - Date.parse(e.started_at)
    if (e.conclusion !== "" && e.conclusion !== "unknown") {
      known++
      if (e.conclusion === "success") ok++
    }
  }
  const rate = known > 0 ? `${Math.floor((ok * 100) / known)}%` : "–"
  const avg = recent.length > 0 ? dur(total / recent.length) : "–"
  const more = entries.length >= limit ? "≥" : ""
  const strip = recent
    .slice(0, STRIP)
    .reverse()
    .map((e) => {
      const [symbol, className] = MARKS[e.conclusion] ?? ["?", "text-muted-foreground"]
      return { id: e.id, symbol, className, label: `#${e.run_number} ${e.job_name}: ${e.conclusion || "unknown"}` }
    })
  return { line: `${more}${recent.length} jobs · ${rate} success · avg ${avg}`, label: win.label, strip }
}
```

- [ ] **Step 4: Implement the repo summary components**

Create `web/src/components/repo-summary.tsx`:

```tsx
import { Button } from "darkraise-ui/components/button"
import { ACTIVITY_LIMIT, useRepoActivity } from "@/api/hooks"
import type { RepoStatus } from "@/api/types"
import { StateBadge } from "@/components/state-badge"
import { summarize } from "@/lib/activity"
import { ago, maxText } from "@/lib/format"
import { errorText } from "@/query"

export function repoState(r: RepoStatus): string {
  if (r.error) return "error"
  if (r.removing) return "removing"
  if (r.paused) return "paused"
  return "active"
}

export function RepoSummary({ repo, now }: { repo: RepoStatus; now: number }) {
  const job = repo.last_job
  const ok = job?.conclusion === "success"
  return (
    <div className="flex flex-col gap-1 text-sm">
      <div className="flex items-center gap-2">
        <StateBadge state={repoState(repo)} />
        <span>
          {repo.active}/{maxText(repo.max)} running · {repo.queued} queued
        </span>
      </div>
      {job ? (
        <p>
          <span role="img" aria-label={ok ? "succeeded" : "failed"} className={ok ? "text-green-600" : "text-destructive"}>
            {ok ? "✔" : "✖"}
          </span>{" "}
          #{job.run_number} {job.job_name} · {ago(now - Date.parse(job.finished_at))}
        </p>
      ) : (
        <p className="text-muted-foreground">no finished jobs yet</p>
      )}
      {repo.removing && <p className="text-amber-600">removing… running jobs finish first</p>}
      {repo.error && <p className="text-destructive">{repo.error}</p>}
    </div>
  )
}

export function ActivitySummary({ name, retention, now }: { name: string; retention: string | undefined; now: number }) {
  const activity = useRepoActivity(name)
  if (activity.isError) {
    return (
      <div className="flex items-center gap-2 text-sm">
        <span className="text-destructive">✖ {errorText(activity.error)}</span>
        <Button size="sm" variant="outline" onClick={() => void activity.refetch()}>
          Retry
        </Button>
      </div>
    )
  }
  if (!activity.data) return <p className="text-sm text-muted-foreground">loading…</p>
  const a = summarize(activity.data, now, retention, ACTIVITY_LIMIT)
  return (
    <div className="flex flex-col gap-1 text-sm">
      <p>
        {a.line}
        <span className="text-muted-foreground"> · {a.label}</span>
      </p>
      {a.strip.length === 0 ? (
        <p className="text-muted-foreground">no finished jobs yet</p>
      ) : (
        <p className="font-mono tracking-wider">
          {a.strip.map((m) => (
            <span key={m.id} role="img" aria-label={m.label} className={m.className}>
              {m.symbol}
            </span>
          ))}
        </p>
      )}
    </div>
  )
}
```

- [ ] **Step 5: Implement the page**

Replace `web/src/pages/repositories.tsx` with:

```tsx
import { useMutation, useQueryClient } from "@tanstack/react-query"
import { Button } from "darkraise-ui/components/button"
import { Card, CardContent, CardHeader, CardTitle } from "darkraise-ui/components/card"
import { EmptyState } from "darkraise-ui/components/empty-state"
import { toast } from "darkraise-ui/components/sonner"
import { Spinner } from "darkraise-ui/components/spinner"
import { PageHeader } from "darkraise-ui/layout"
import { useState } from "react"
import { api } from "@/api/client"
import { keys, useConfig, useStatus } from "@/api/hooks"
import { ConfirmDialog, type Confirm } from "@/components/confirm-dialog"
import { ActivitySummary, RepoSummary } from "@/components/repo-summary"
import { useNow } from "@/lib/use-now"

export function RepositoriesPage() {
  const status = useStatus()
  const config = useConfig()
  const now = useNow()
  const queryClient = useQueryClient()
  const [confirm, setConfirm] = useState<Confirm | null>(null)
  const refresh = () =>
    Promise.all([
      queryClient.invalidateQueries({ queryKey: keys.status }),
      queryClient.invalidateQueries({ queryKey: keys.config }),
    ])
  const pause = useMutation({
    mutationFn: ({ name, resume }: { name: string; resume: boolean }) => (resume ? api.resumeRepo(name) : api.pauseRepo(name)),
    onSuccess: (_data, { name, resume }) => {
      toast.success(`${resume ? "resumed" : "paused"} ${name}`)
    },
    onSettled: refresh,
  })
  const remove = useMutation({
    mutationFn: (name: string) => api.removeRepo(name),
    onSuccess: (_data, name) => {
      toast.success(`removing ${name}`)
    },
    onSettled: refresh,
  })

  const st = status.data
  if (!st) {
    return (
      <>
        <PageHeader title="Repositories" />
        <Spinner label="waiting for the daemon…" />
      </>
    )
  }
  const offline = status.isError
  return (
    <>
      <PageHeader title="Repositories" />
      {st.repos.length === 0 ? (
        <EmptyState title="No repositories yet" />
      ) : (
        <div className="grid gap-4 lg:grid-cols-2">
          {st.repos.map((r) => (
            <Card key={r.name}>
              <CardHeader>
                <CardTitle>{r.name}</CardTitle>
              </CardHeader>
              <CardContent className="flex flex-col gap-3">
                <RepoSummary repo={r} now={now} />
                <ActivitySummary name={r.name} retention={config.data?.history_retention} now={now} />
                <div className="flex flex-wrap gap-2">
                  <Button
                    size="sm"
                    variant="secondary"
                    aria-label={`${r.paused ? "Resume" : "Pause"} ${r.name}`}
                    disabled={offline || Boolean(r.removing)}
                    onClick={() => pause.mutate({ name: r.name, resume: r.paused })}
                  >
                    {r.paused ? "Resume" : "Pause"}
                  </Button>
                  <Button
                    size="sm"
                    variant="destructive"
                    aria-label={`Remove ${r.name}`}
                    disabled={offline || Boolean(r.removing)}
                    onClick={() =>
                      setConfirm({
                        title: `Remove repo ${r.name}? Its running jobs finish first.`,
                        action: "Remove",
                        destructive: true,
                        run: () => remove.mutate(r.name),
                      })
                    }
                  >
                    Remove
                  </Button>
                </div>
              </CardContent>
            </Card>
          ))}
        </div>
      )}
      <ConfirmDialog confirm={confirm} onClose={() => setConfirm(null)} />
    </>
  )
}
```

- [ ] **Step 6: Run the checks**

Run: `timeout 300 npm --prefix web test -- src/lib/activity.test.ts src/pages/repositories.test.tsx`, then the four web checks.
Expected: all PASS. `router.test.tsx`'s `serves /repositories` still passes: its mock answers only `/auth/state`, so the page shows its header over the waiting spinner.

- [ ] **Step 7: Commit**

```bash
git add web/src/lib/activity.ts web/src/lib/activity.test.ts web/src/components/repo-summary.tsx web/src/pages/repositories.tsx web/src/pages/repositories.test.tsx
git commit -m "feat(web): add the Repositories page"
```

### Task 7: web: Add repository dialog

**Files:**
- Create: `web/src/components/add-repo-dialog.tsx`
- Modify: `web/src/pages/repositories.tsx` (from Task 6: header button, empty-state action, dialog)
- Test: `web/src/components/add-repo-dialog.test.tsx`

**Interfaces:**
- Consumes: C2 (`AddRepoRequest`), C3 (`addRepo`), C5 (`useAvailableRepos`, `keys`), C1 (`useStatus`), C6 (`TagField`), Task 6's `RepositoriesPage`.
- Produces: `AddRepoDialog({ open, onClose }: { open: boolean; onClose: () => void })` (C10).

**Items:** 11
**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 0 = 3

- [ ] **Step 1: Write the failing test**

Create `web/src/components/add-repo-dialog.test.tsx`:

```tsx
import { screen, waitFor, within } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import { json, mockApi, noContent } from "@/test/api"
import { authedRoutes, fixtures } from "@/test/fixtures"
import { renderApp } from "@/test/render"

function routes(over: Record<string, unknown> = {}) {
  return authedRoutes({ "GET /api/history": [], "GET /api/repos/available": fixtures.availableRepos, ...over })
}

async function open() {
  const view = renderApp("/repositories")
  await view.user.click(await screen.findByRole("button", { name: "+ Add repository" }))
  const dialog = within(await screen.findByRole("dialog"))
  await dialog.findByRole("option", { name: /new-repo/ })
  return { ...view, dialog }
}

describe("Add repository dialog", () => {
  it("lists the token's repos, with configured ones not pickable", async () => {
    mockApi(routes())
    const { dialog, user } = await open()
    expect(dialog.getByRole("option", { name: /darkmem/ })).toBeDisabled()
    expect(dialog.getByText("added")).toBeInTheDocument()
    expect(dialog.getByRole("button", { name: "Add" })).toBeDisabled()
    expect(dialog.getByText("pick one below")).toBeInTheDocument()
    expect(dialog.getByPlaceholderText("1 (default)")).toBeInTheDocument()
    expect(dialog.getByText("⚠ self-hosted runners on a public repo can run anyone's code")).toBeInTheDocument()
    await user.type(dialog.getByRole("textbox", { name: "Filter repositories" }), "zzz")
    expect(dialog.getByText("no match")).toBeInTheDocument()
  })

  it("says when the token sees no repo", async () => {
    mockApi(routes({ "GET /api/repos/available": [] }))
    const { user } = renderApp("/repositories")
    await user.click(await screen.findByRole("button", { name: "+ Add repository" }))
    expect(await within(await screen.findByRole("dialog")).findByText("nothing to pick")).toBeInTheDocument()
  })

  it("adds a repo with only the fields that were set", async () => {
    const { calls } = mockApi(routes({ "POST /api/repos": () => noContent() }))
    const { dialog, user } = await open()
    await user.click(dialog.getByRole("option", { name: /new-repo/ }))
    await user.click(dialog.getByRole("button", { name: "Add" }))
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull())
    expect(calls.find((c) => c.method === "POST")?.body).toEqual({ name: "new-repo", allow_public: false })
    expect((await screen.findAllByText("added new-repo")).length).toBeGreaterThan(0)
  })

  it("sends max, labels and the public override when set", async () => {
    const { calls } = mockApi(routes({ "POST /api/repos": () => noContent() }))
    const { dialog, user } = await open()
    await user.click(dialog.getByRole("option", { name: /new-repo/ }))
    await user.type(dialog.getByLabelText("Max"), "2")
    await user.type(dialog.getByRole("textbox", { name: "Labels" }), "gpu{Enter}")
    await user.click(dialog.getByRole("switch", { name: "Allow public repo" }))
    await user.click(dialog.getByRole("button", { name: "Add" }))
    await waitFor(() => expect(calls.some((c) => c.method === "POST")).toBe(true))
    expect(calls.find((c) => c.method === "POST")?.body).toEqual({ name: "new-repo", max: 2, labels: ["gpu"], allow_public: true })
  })

  it("keeps the dialog open with the daemon's reason on failure", async () => {
    mockApi(routes({ "POST /api/repos": () => json({ error: "repo new-repo is already configured" }, 409) }))
    const { dialog, user } = await open()
    await user.click(dialog.getByRole("option", { name: /new-repo/ }))
    await user.click(dialog.getByRole("button", { name: "Add" }))
    expect(await dialog.findByText("✖ repo new-repo is already configured")).toBeInTheDocument()
    expect(screen.getByRole("dialog")).toBeInTheDocument()
  })

  it("is offered from the empty page too", async () => {
    mockApi(routes({ "GET /api/status": { ...fixtures.status, repos: [] } }))
    renderApp("/repositories")
    await screen.findByText("No repositories yet")
    expect(screen.getAllByRole("button", { name: "+ Add repository" })).toHaveLength(2)
  })
})
```

- [ ] **Step 2: Run it to see it fail**

Run: `timeout 300 npm --prefix web test -- src/components/add-repo-dialog.test.tsx`
Expected: FAIL; there is no `+ Add repository` button.

- [ ] **Step 3: Implement the dialog**

Create `web/src/components/add-repo-dialog.tsx`:

```tsx
import { useQueryClient } from "@tanstack/react-query"
import { Badge } from "darkraise-ui/components/badge"
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
import { Switch } from "darkraise-ui/components/switch"
import { useState } from "react"
import { api } from "@/api/client"
import { keys, useAvailableRepos, useStatus } from "@/api/hooks"
import type { AddRepoRequest } from "@/api/types"
import { TagField } from "@/components/tag-field"
import { errorText } from "@/query"

function clampMax(text: string): number {
  return Math.min(99, Math.max(0, Math.trunc(Number(text))))
}

export function AddRepoDialog({ open, onClose }: { open: boolean; onClose: () => void }) {
  const repos = useAvailableRepos(open)
  const status = useStatus()
  const queryClient = useQueryClient()
  const [filter, setFilter] = useState("")
  const [picked, setPicked] = useState("")
  const [max, setMax] = useState("")
  const [labels, setLabels] = useState<string[]>([])
  const [allowPublic, setAllowPublic] = useState(false)
  const [error, setError] = useState("")
  const [busy, setBusy] = useState(false)

  function close() {
    setFilter("")
    setPicked("")
    setMax("")
    setLabels([])
    setAllowPublic(false)
    setError("")
    onClose()
  }

  async function add() {
    if (status.isError) return setError("the daemon is unreachable")
    if (!picked) return setError("pick a repository first")
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
    toast.success(`added ${picked}`)
    void queryClient.invalidateQueries({ queryKey: keys.status })
    void queryClient.invalidateQueries({ queryKey: keys.config })
    close()
  }

  const needle = filter.trim().toLowerCase()
  const items = (repos.data ?? []).filter((r) => r.name.toLowerCase().includes(needle))
  let picker
  if (repos.isError) {
    picker = (
      <div className="flex items-center gap-2 text-sm">
        <span className="text-destructive">✖ {errorText(repos.error)}</span>
        <Button size="sm" variant="outline" onClick={() => void repos.refetch()}>
          Retry
        </Button>
      </div>
    )
  } else if (!repos.data) {
    picker = <p className="text-sm text-muted-foreground">loading repositories…</p>
  } else {
    picker = (
      <div className="flex flex-col gap-2">
        <Input aria-label="Filter repositories" placeholder="type to filter" value={filter} onChange={(e) => setFilter(e.target.value)} />
        <div role="listbox" aria-label="Repositories" className="max-h-64 overflow-auto rounded-md border">
          {repos.data.length === 0 && <p className="p-2 text-sm text-muted-foreground">nothing to pick</p>}
          {repos.data.length > 0 && items.length === 0 && <p className="p-2 text-sm text-muted-foreground">no match</p>}
          {items.map((r) => (
            <button
              key={r.name}
              type="button"
              role="option"
              aria-selected={picked === r.name}
              disabled={r.configured}
              onClick={() => setPicked(r.name)}
              className="flex w-full items-center gap-2 px-2 py-1 text-left text-sm hover:bg-muted disabled:opacity-50 aria-selected:bg-muted"
            >
              <span>{r.name}</span>
              <Badge variant={r.private ? "secondary" : "amber"} size="sm">
                {r.private ? "private" : "public"}
              </Badge>
              {r.configured && <span className="text-muted-foreground">added</span>}
            </button>
          ))}
        </div>
      </div>
    )
  }

  return (
    <Dialog
      open={open}
      onOpenChange={(o) => {
        if (!o) close()
      }}
    >
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Add repository</DialogTitle>
        </DialogHeader>
        <DialogBody className="flex flex-col gap-4">
          <div className="flex flex-col gap-1">
            <span className="text-sm font-medium">Repository</span>
            {picked ? <strong>{picked}</strong> : <span className="text-sm text-muted-foreground">pick one below</span>}
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
          <p className="text-sm text-amber-600">⚠ self-hosted runners on a public repo can run anyone's code</p>
          {error && (
            <p role="alert" className="text-sm text-destructive">
              ✖ {error}
            </p>
          )}
        </DialogBody>
        <DialogFooter>
          <Button variant="outline" onClick={close}>
            Cancel
          </Button>
          <Button disabled={!picked || busy || status.isError} onClick={() => void add()}>
            {busy ? "Adding…" : "Add"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
```

- [ ] **Step 4: Offer it on the Repositories page**

In `web/src/pages/repositories.tsx`, add the import below the `ConfirmDialog` import:

```tsx
import { AddRepoDialog } from "@/components/add-repo-dialog"
```

Below `const [confirm, setConfirm] = useState<Confirm | null>(null)`, add:

```tsx
  const [adding, setAdding] = useState(false)
```

Below `const offline = status.isError`, add:

```tsx
  const addButton = (
    <Button disabled={offline} onClick={() => setAdding(true)}>
      + Add repository
    </Button>
  )
```

Replace `      <PageHeader title="Repositories" />\n      {st.repos.length === 0 ? (\n        <EmptyState title="No repositories yet" />` (the second `PageHeader`, after the spinner branch) with:

```tsx
      <PageHeader title="Repositories" actions={addButton} />
      {st.repos.length === 0 ? (
        <EmptyState title="No repositories yet" action={addButton} />
```

Replace `      <ConfirmDialog confirm={confirm} onClose={() => setConfirm(null)} />` with:

```tsx
      <ConfirmDialog confirm={confirm} onClose={() => setConfirm(null)} />
      <AddRepoDialog open={adding} onClose={() => setAdding(false)} />
```

- [ ] **Step 5: Run the checks**

Run: `timeout 300 npm --prefix web test -- src/components/add-repo-dialog.test.tsx src/pages/repositories.test.tsx`, then the four web checks.
Expected: all PASS. If `findByRole("dialog")` finds nothing, read `DialogContent`'s rendered role in `web/node_modules/darkraise-ui/dist` and query that role instead; record it in the commit body.

- [ ] **Step 6: Commit**

```bash
git add web/src/components/add-repo-dialog.tsx web/src/components/add-repo-dialog.test.tsx web/src/pages/repositories.tsx
git commit -m "feat(web): add the Add repository dialog"
```

### Task 8: web: repository settings page

**Files:**
- Create: `web/src/pages/repository.tsx`
- Modify: `web/src/router.tsx` (route), `web/src/pages/repositories.tsx` (from Task 7: Manage link)
- Test: `web/src/pages/repository.test.tsx`

**Interfaces:**
- Consumes: C2 (`RepoPatch`), C3 (`patchConfig`, `config`), C5, C6 (`TagField`), C8 (`changedKeys`, `settle`, `sameValue`, `Values`, `SaveBar`, `RejectedAlert`, `UnsavedGuard`), Task 6's `RepoSummary` and `ActivitySummary`.
- Produces: the `/repositories/$name` route and `RepositoryPage` (C9, C10). Tasks 9 and 10 edit `web/src/pages/repository.tsx`, which this task leaves holding `SYSTEM_LABELS`, `values`, `setDraft`, `custom` (from `customLabels`), `status`, `locked`, and a `<SaveBar` element at the top level of the returned fragment.

**Items:** 11, 13
**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 1 = 4

- [ ] **Step 1: Write the failing test**

Create `web/src/pages/repository.test.tsx`:

```tsx
import { screen, waitFor } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import type { Config } from "@/api/types"
import { json, mockApi, noContent } from "@/test/api"
import { authedRoutes, fixtures } from "@/test/fixtures"
import { renderApp } from "@/test/render"

function routes(over: Record<string, unknown> = {}) {
  return authedRoutes({ "GET /api/history": [], ...over })
}

// The daemon applies a PATCH, so the config read after it reflects the save.
function applyingRoutes() {
  let config: Config = fixtures.config
  return routes({
    "GET /api/config": () => config,
    "PATCH /api/config": ({ body }: { body: unknown }) => {
      const patch = (body as { repos: Record<string, Record<string, unknown>> }).repos.darkmem
      config = { ...config, repos: (config.repos ?? []).map((r) => (r.name === "darkmem" ? { ...r, ...patch } : r)) }
      return noContent()
    },
  })
}

describe("repository page", () => {
  it("shows the repo's settings and the labels its runners get", async () => {
    mockApi(routes())
    renderApp("/repositories/darkmem")
    expect(await screen.findByLabelText("Max")).toHaveValue(2)
    expect(screen.getByLabelText("Warm")).toHaveValue(1)
    expect(screen.getByRole("button", { name: "Remove gpu" })).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Remove darkmem-" })).toBeInTheDocument()
    expect(screen.getByText("self-hosted linux x64")).toBeInTheDocument()
    expect(screen.getByText("homelab (global) gpu")).toBeInTheDocument()
    expect(screen.getByText("1/2 running · 3 queued")).toBeInTheDocument()
  })

  it("saves only the changed field", async () => {
    const { calls } = mockApi(applyingRoutes())
    const { user } = renderApp("/repositories/darkmem")
    const max = await screen.findByLabelText("Max")
    await user.clear(max)
    await user.type(max, "3")
    expect(screen.getByText("● 1 unsaved change")).toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: "Save changes" }))
    expect((await screen.findAllByText("Repositories saved")).length).toBeGreaterThan(0)
    expect(calls.find((c) => c.method === "PATCH")?.body).toEqual({ repos: { darkmem: { max: 3 } } })
    await waitFor(() => expect(screen.queryByText("● 1 unsaved change")).toBeNull())
    expect(screen.queryByText(/daemon did not apply/)).toBeNull()
  })

  it("says when the daemon did not apply a field", async () => {
    mockApi(routes({ "PATCH /api/config": () => noContent() }))
    const { user } = renderApp("/repositories/darkmem")
    await user.type(await screen.findByRole("textbox", { name: "Prefixes" }), "test_{Enter}")
    await user.click(screen.getByRole("button", { name: "Save changes" }))
    expect(
      (await screen.findAllByText("daemon did not apply darkmem.cleanup_name_prefixes; is it older than this ghr?")).length,
    ).toBeGreaterThan(0)
  })

  it("shows a rejected save and keeps the edit", async () => {
    mockApi(
      routes({
        "PATCH /api/config": () =>
          json({ error: "darkmem: needs at least one label in labels or repo labels; a; b; c" }, 400),
      }),
    )
    const { user } = renderApp("/repositories/darkmem")
    await user.click(await screen.findByRole("button", { name: "Remove gpu" }))
    await user.click(screen.getByRole("button", { name: "Save changes" }))
    expect(await screen.findByText("Save rejected")).toBeInTheDocument()
    expect(screen.getByText("✖ darkmem: needs at least one label in labels or repo labels")).toBeInTheDocument()
    expect(screen.getByText("… and 1 more")).toBeInTheDocument()
    expect((await screen.findAllByText("repositories not saved")).length).toBeGreaterThan(0)
    expect(screen.getByText("● 1 unsaved change")).toBeInTheDocument()
  })

  it("blocks a save while warm exceeds max", async () => {
    const { calls } = mockApi(routes())
    const { user } = renderApp("/repositories/darkmem")
    const warm = await screen.findByLabelText("Warm")
    await user.clear(warm)
    await user.type(warm, "3")
    expect(screen.getByText("✖ warm must be <= max")).toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: "Save changes" }))
    expect((await screen.findAllByText("fix the highlighted settings first")).length).toBeGreaterThan(0)
    expect(calls.some((c) => c.method === "PATCH")).toBe(false)
  })

  it("keeps an emptied box empty and refuses to save it", async () => {
    const { calls } = mockApi(routes())
    const { user } = renderApp("/repositories/darkmem")
    const max = await screen.findByLabelText("Max")
    await user.clear(max)
    expect(max).toHaveValue(null)
    expect(screen.getByText("✖ enter a number from 0 to 99")).toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: "Save changes" }))
    expect((await screen.findAllByText("fix the highlighted settings first")).length).toBeGreaterThan(0)
    expect(calls.some((c) => c.method === "PATCH")).toBe(false)
  })

  it("disables the form while the daemon is unreachable", async () => {
    mockApi(routes({ "GET /api/status": () => json({ error: "connection refused" }, 502) }))
    renderApp("/repositories/darkmem")
    await waitFor(() => expect(screen.getByLabelText("Max")).toBeDisabled())
    expect(screen.getByRole("textbox", { name: "Prefixes" })).toBeDisabled()
  })

  it("discards the edits", async () => {
    mockApi(routes())
    const { user } = renderApp("/repositories/darkmem")
    const max = await screen.findByLabelText("Max")
    await user.clear(max)
    await user.type(max, "5")
    await user.click(screen.getByRole("button", { name: "Discard" }))
    expect(screen.getByLabelText("Max")).toHaveValue(2)
    expect(screen.queryByText("● 1 unsaved change")).toBeNull()
  })

  it("asks before leaving with unsaved edits", async () => {
    mockApi(routes())
    const { user } = renderApp("/repositories/darkmem")
    const max = await screen.findByLabelText("Max")
    await user.clear(max)
    await user.type(max, "5")
    await user.click(screen.getByRole("link", { name: "Runners" }))
    expect(await screen.findByText("You have 1 unsaved change on the Repositories page.")).toBeInTheDocument()
  })

  it("locks the form while the repo is being removed", async () => {
    const repos = fixtures.status.repos.map((r) => (r.name === "darkmem" ? { ...r, removing: true } : r))
    mockApi(routes({ "GET /api/status": { ...fixtures.status, repos } }))
    renderApp("/repositories/darkmem")
    expect(await screen.findByLabelText("Max")).toBeDisabled()
    expect(screen.getByRole("textbox", { name: "Repo labels" })).toBeDisabled()
  })

  it("says when the repo is not configured", async () => {
    mockApi(routes())
    renderApp("/repositories/nope")
    expect(await screen.findByText("Repository not found")).toBeInTheDocument()
  })

  it("is reached from the Repositories page", async () => {
    mockApi(routes())
    const { user, router } = renderApp("/repositories")
    await user.click(await screen.findByRole("link", { name: "Manage darkmem" }))
    await waitFor(() => expect(router.state.location.pathname).toBe("/repositories/darkmem"))
  })
})
```

- [ ] **Step 2: Run it to see it fail**

Run: `timeout 300 npm --prefix web test -- src/pages/repository.test.tsx`
Expected: FAIL; `/repositories/darkmem` matches no route.

- [ ] **Step 3: Implement the page**

Create `web/src/pages/repository.tsx`:

```tsx
import { useQueryClient } from "@tanstack/react-query"
import { useParams } from "@tanstack/react-router"
import { Card, CardContent, CardHeader, CardTitle } from "darkraise-ui/components/card"
import { EmptyState } from "darkraise-ui/components/empty-state"
import { Input } from "darkraise-ui/components/input"
import { Label } from "darkraise-ui/components/label"
import { toast } from "darkraise-ui/components/sonner"
import { Spinner } from "darkraise-ui/components/spinner"
import { PageHeader } from "darkraise-ui/layout"
import { useState, type ReactNode } from "react"
import { ApiError, api } from "@/api/client"
import { keys, useConfig, useStatus } from "@/api/hooks"
import type { RepoConfig, RepoPatch } from "@/api/types"
import { ActivitySummary, RepoSummary } from "@/components/repo-summary"
import { RejectedAlert, SaveBar } from "@/components/save-bar"
import { TagField } from "@/components/tag-field"
import { UnsavedGuard } from "@/components/unsaved-guard"
import { changedKeys, sameValue, settle, type Values } from "@/lib/draft"
import { useNow } from "@/lib/use-now"
import { errorText } from "@/query"

const SYSTEM_LABELS = ["self-hosted", "linux", "x64"]

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

function Row({ label, htmlFor, desc, error, children }: { label: string; htmlFor?: string; desc?: string; error?: string; children: ReactNode }) {
  return (
    <div className="grid gap-1 sm:grid-cols-[10rem_1fr] sm:items-start">
      {htmlFor ? (
        <Label htmlFor={htmlFor} className="pt-2">
          {label}
        </Label>
      ) : (
        <span className="pt-2 text-sm font-medium">{label}</span>
      )}
      <div className="flex flex-col gap-1">
        {children}
        {desc && <p className="text-xs text-muted-foreground">{desc}</p>}
        {error && <p className="text-xs text-destructive">✖ {error}</p>}
      </div>
    </div>
  )
}

export function RepositoryPage() {
  const { name = "" } = useParams({ strict: false }) as { name?: string }
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
    key in draft && draft[key] === undefined && loaded[key] !== undefined ? "enter a number from 0 to 99" : ""
  const maxError = blank("max")
  const warmError =
    blank("warm") || (maxValue !== undefined && maxValue > 0 && warmValue > maxValue ? "warm must be <= max" : "")
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
      toast.error("fix the highlighted settings first")
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
        toast.error("repositories not saved")
      } else {
        toast.error(`repositories not saved: ${errorText(err)}`)
      }
      setSaving(false)
      return false
    }
    toast.success("Repositories saved")
    try {
      const fresh = await queryClient.fetchQuery({ queryKey: keys.config, queryFn: ({ signal }) => api.config(signal), staleTime: 0 })
      const applied = repoValues(fresh.repos?.find((r) => r.name === name))
      for (const key of Object.keys(sent)) {
        if (!sameValue(applied[key], sent[key])) toast.error(`daemon did not apply ${name}.${key}; is it older than this ghr?`)
      }
    } catch (err) {
      toast.error(`saved, but re-reading the config failed: ${errorText(err)}`)
    }
    setDraft((d) => settle(d, sent))
    setSaving(false)
    return true
  }

  const breadcrumbs = [{ label: "Repositories", href: "/repositories" }, { label: name }]
  if (!cfg) {
    return (
      <>
        <PageHeader breadcrumbs={breadcrumbs} title={name} />
        <Spinner label="loading…" />
      </>
    )
  }
  if (!repoCfg) {
    return (
      <>
        <PageHeader breadcrumbs={breadcrumbs} title={name} />
        <EmptyState title="Repository not found" description={`${name} is not in ghr's config; it may have been removed.`} />
      </>
    )
  }

  const custom = customLabels(cfg.labels ?? [], values.labels as string[])
  const defaultMax = cfg.mode === "all" ? "∞ (default)" : "1 (default)"

  return (
    <>
      <PageHeader breadcrumbs={breadcrumbs} title={name} />
      {rejection && <RejectedAlert message={rejection} />}
      <div className="grid gap-4 lg:grid-cols-2">
        <Card>
          <CardHeader>
            <CardTitle>Summary</CardTitle>
          </CardHeader>
          <CardContent className="flex flex-col gap-3">
            {repoStatus && <RepoSummary repo={repoStatus} now={now} />}
            <ActivitySummary name={name} retention={cfg.history_retention} now={now} />
          </CardContent>
        </Card>
        <Card>
          <CardHeader>
            <CardTitle>Capacity</CardTitle>
          </CardHeader>
          <CardContent className="flex flex-col gap-4">
            <Row label="Max" htmlFor="repo-max" desc="once set, it stays explicit" error={maxError}>
              <Input
                id="repo-max"
                type="number"
                min={0}
                max={99}
                placeholder={defaultMax}
                disabled={locked}
                value={typeof values.max === "number" ? String(values.max) : ""}
                onChange={(e) => setNumber("max", e.target.value)}
              />
            </Row>
            <Row label="Warm" htmlFor="repo-warm" desc="applies in all mode" error={warmError}>
              <Input
                id="repo-warm"
                type="number"
                min={0}
                max={99}
                placeholder="1 (default)"
                disabled={locked}
                value={typeof values.warm === "number" ? String(values.warm) : ""}
                onChange={(e) => setNumber("warm", e.target.value)}
              />
            </Row>
          </CardContent>
        </Card>
        <Card>
          <CardHeader>
            <CardTitle>Labels</CardTitle>
          </CardHeader>
          <CardContent className="flex flex-col gap-4">
            <Row label="Repo labels" desc="added to this repo's runners">
              <TagField
                label="Repo labels"
                value={values.labels as string[]}
                disabled={locked}
                onChange={(next) => setDraft((d) => ({ ...d, labels: next }))}
              />
            </Row>
            <Row label="Runners get">
              <div className="flex flex-col gap-1 pt-2 text-sm">
                <p>{SYSTEM_LABELS.join(" ")}</p>
                {custom.length > 0 && <p>{custom.map((c) => (c.global ? `${c.label} (global)` : c.label)).join("  ")}</p>}
                <p className="text-muted-foreground">runners already running keep their labels</p>
              </div>
            </Row>
          </CardContent>
        </Card>
        <Card>
          <CardHeader>
            <CardTitle>Cleanup</CardTitle>
          </CardHeader>
          <CardContent>
            <Row label="Prefixes" desc="container name prefixes removed after each job">
              <TagField
                label="Prefixes"
                value={values.cleanup_name_prefixes as string[]}
                disabled={locked}
                onChange={(next) => setDraft((d) => ({ ...d, cleanup_name_prefixes: next }))}
              />
            </Row>
          </CardContent>
        </Card>
      </div>
      <SaveBar count={changed.length} saving={saving} disabled={status.isError} onSave={() => void save()} onDiscard={discard} />
      <UnsavedGuard count={changed.length} page="Repositories" saving={saving} onSave={save} onDiscard={discard} />
    </>
  )
}
```

- [ ] **Step 4: Register the route**

In `web/src/router.tsx`, add below `import { RepositoriesPage } from "./pages/repositories"`:

```tsx
import { RepositoryPage } from "./pages/repository"
```

Below the `repositoriesRoute` line, add:

```tsx
const repositoryRoute = createRoute({ getParentRoute: () => appRoute, path: "/repositories/$name", component: RepositoryPage })
```

Replace `  appRoute.addChildren([dashboardRoute, repositoriesRoute, runnersRoute, runnerRoute, historyRoute, storageRoute, settingsRoute]),` with:

```tsx
  appRoute.addChildren([
    dashboardRoute,
    repositoriesRoute,
    repositoryRoute,
    runnersRoute,
    runnerRoute,
    historyRoute,
    storageRoute,
    settingsRoute,
  ]),
```

- [ ] **Step 5: Link each card to its page**

In `web/src/pages/repositories.tsx`, add `import { Link } from "@tanstack/react-router"` below the `@tanstack/react-query` import, and replace `                <div className="flex flex-wrap gap-2">` with:

```tsx
                <div className="flex flex-wrap gap-2">
                  <Button size="sm" variant="outline" asChild>
                    <Link to="/repositories/$name" params={{ name: r.name }} aria-label={`Manage ${r.name}`}>
                      Manage
                    </Link>
                  </Button>
```

- [ ] **Step 6: Run the checks**

Run: `timeout 300 npm --prefix web test -- src/pages/repository.test.tsx src/pages/repositories.test.tsx src/router.test.tsx`, then the four web checks.
Expected: all PASS.

- [ ] **Step 7: Commit**

```bash
git add web/src/pages/repository.tsx web/src/pages/repository.test.tsx web/src/router.tsx web/src/pages/repositories.tsx
git commit -m "feat(web): add the repository settings page"
```

### Task 9: web: workflow labels card

**Files:**
- Create: `web/src/lib/labels.ts`, `web/src/components/label-check-card.tsx`
- Modify: `web/src/pages/repository.tsx` (from Task 8)
- Test: `web/src/lib/labels.test.ts`, `web/src/components/label-check-card.test.tsx`

**Interfaces:**
- Consumes: C3 (`startLabelCheck`), C5 (`useLabelCheck`, `keys`), Task 8's `RepositoryPage` (`SYSTEM_LABELS`, `values`, `setDraft`, `custom`, `status`, `locked`, `<SaveBar`); C1 (`useStatus`).
- Produces: `web/src/lib/labels.ts` exports `OS_ARCH: ReadonlySet<string>`, `matchLabels(jobLabels: string[], effective: string[]): boolean` and `classify(labels: string[], effective: string[]): { kind: "matched" | "unmatched" | "other"; missing: string[] }`. `LabelCheckCard({ name, effective, degraded, degradedReason, disabled, onAddLabel }: { name: string; effective: string[]; degraded: boolean; degradedReason?: string; disabled: boolean; onAddLabel: (label: string) => void })`. `RepositoryPage` gains `degraded` (Task 10 reads it) and a `<LabelCheckCard … onAddLabel={addLabel} />` line.

**Items:** 11
**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 0 = 3

- [ ] **Step 1: Write the failing tests**

Create `web/src/lib/labels.test.ts`:

```ts
import { describe, expect, it } from "vitest"
import { classify, matchLabels } from "./labels"

const effective = ["self-hosted", "linux", "x64", "homelab", "gpu"]

describe("matchLabels", () => {
  it("needs every job label among the effective ones, ignoring case", () => {
    expect(matchLabels(["Self-Hosted", "GPU"], effective)).toBe(true)
    expect(matchLabels(["self-hosted", "big"], effective)).toBe(false)
  })
})

describe("classify", () => {
  it("matches what ghr takes", () => {
    expect(classify(["self-hosted", "homelab"], effective)).toEqual({ kind: "matched", missing: [] })
  })

  it("lists what a self-hosted group misses", () => {
    expect(classify(["self-hosted", "arm64", "big"], effective)).toEqual({ kind: "unmatched", missing: ["arm64", "big"] })
  })

  it("leaves runner groups and GitHub-hosted jobs alone", () => {
    expect(classify([], effective)).toEqual({ kind: "other", missing: [] })
    expect(classify(["ubuntu-latest"], effective)).toEqual({ kind: "other", missing: [] })
  })
})
```

Create `web/src/components/label-check-card.test.tsx`:

```tsx
import { screen, waitFor } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import type { LabelCheck } from "@/api/types"
import { mockApi } from "@/test/api"
import { authedRoutes, fixtures } from "@/test/fixtures"
import { renderApp } from "@/test/render"

const unmatched: LabelCheck = {
  state: "done",
  checked_at: new Date().toISOString(),
  partial: true,
  groups: [{ labels: ["self-hosted", "arm64", "big"], jobs: ["ci / build"], more: 2, count: 4, last_seen: new Date().toISOString() }],
}

function routes(over: Record<string, unknown> = {}) {
  return authedRoutes({
    "GET /api/history": [],
    "GET /api/repos/darkmem/registrations": [],
    "GET /api/repos/darkmem/label-check": fixtures.labelCheck,
    ...over,
  })
}

describe("Workflow labels card", () => {
  it("shows the label groups and whether ghr takes them", async () => {
    mockApi(routes())
    renderApp("/repositories/darkmem")
    expect(await screen.findByText("homelab, self-hosted")).toBeInTheDocument()
    expect(screen.getByText("matched")).toBeInTheDocument()
    expect(screen.getByText("ci / build, ci / test")).toBeInTheDocument()
    expect(screen.getByText(/^Checked /)).toBeInTheDocument()
  })

  it("offers the missing labels and adds one as an unsaved edit", async () => {
    const { calls } = mockApi(routes({ "GET /api/repos/darkmem/label-check": unmatched }))
    const { user } = renderApp("/repositories/darkmem")
    expect(await screen.findByText("missing arm64, big")).toBeInTheDocument()
    expect(screen.getByText(/ · partial$/)).toBeInTheDocument()
    expect(screen.getByText("ci / build +2 more")).toBeInTheDocument()
    expect(screen.getByText("needs a different OS or architecture")).toBeInTheDocument()
    expect(screen.queryByRole("button", { name: "+ add arm64" })).toBeNull()
    expect(
      screen.getByText("Adding a label changes which jobs ghr accepts; it does not install anything on the runner."),
    ).toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: "+ add big" }))
    expect(screen.getByRole("button", { name: "Remove big" })).toBeInTheDocument()
    expect(screen.getByText("● 1 unsaved change")).toBeInTheDocument()
    expect(calls.some((c) => c.method !== "GET")).toBe(false)
  })

  it("starts a check and polls it", async () => {
    let started = false
    const { calls } = mockApi(
      routes({
        "POST /api/repos/darkmem/label-check": () => {
          started = true
          return new Response(null, { status: 202 })
        },
        "GET /api/repos/darkmem/label-check": (): LabelCheck =>
          started ? { state: "checking", partial: false, groups: [] } : { state: "not_checked", partial: false, groups: [] },
      }),
    )
    const { user } = renderApp("/repositories/darkmem")
    expect(await screen.findByText("Not checked yet")).toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: "Check now" }))
    expect(await screen.findByText("Checking…")).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Check now" })).toBeDisabled()
    await waitFor(() => expect(calls.some((c) => c.method === "POST")).toBe(true))
  })

  it("does not read while GitHub rejects the token", async () => {
    const { calls } = mockApi(
      routes({ "GET /api/status": { ...fixtures.status, degraded: true, degraded_reason: "bad credentials" } }),
    )
    renderApp("/repositories/darkmem")
    expect((await screen.findAllByText("GitHub is rejecting the token: bad credentials")).length).toBeGreaterThan(0)
    expect(calls.some((c) => c.path.endsWith("/label-check"))).toBe(false)
  })
})
```

- [ ] **Step 2: Run them to see them fail**

Run: `timeout 300 npm --prefix web test -- src/lib/labels.test.ts src/components/label-check-card.test.tsx`
Expected: FAIL; `./labels` cannot be resolved, and the page has no Workflow labels card.

- [ ] **Step 3: Implement the label logic**

Create `web/src/lib/labels.ts`:

```ts
export const OS_ARCH: ReadonlySet<string> = new Set(["linux", "windows", "macos", "x64", "x86", "arm", "arm64"])

// The daemon's own predicate (sched.MatchLabels).
export function matchLabels(jobLabels: string[], effective: string[]): boolean {
  const set = new Set(effective.map((l) => l.toLowerCase()))
  return jobLabels.every((l) => set.has(l.toLowerCase()))
}

// A group that asks for self-hosted but misses labels is one ghr could take;
// any other mismatch is a job meant for another runner.
export function classify(labels: string[], effective: string[]): { kind: "matched" | "unmatched" | "other"; missing: string[] } {
  if (labels.length === 0) return { kind: "other", missing: [] }
  if (matchLabels(labels, effective)) return { kind: "matched", missing: [] }
  const has = new Set(effective.map((l) => l.toLowerCase()))
  if (!labels.includes("self-hosted")) return { kind: "other", missing: [] }
  return { kind: "unmatched", missing: labels.filter((l) => !has.has(l)) }
}
```

- [ ] **Step 4: Implement the card**

Create `web/src/components/label-check-card.tsx`:

```tsx
import { useQueryClient } from "@tanstack/react-query"
import { Button } from "darkraise-ui/components/button"
import { Card, CardContent, CardHeader, CardTitle } from "darkraise-ui/components/card"
import { toast } from "darkraise-ui/components/sonner"
import { useState } from "react"
import { api } from "@/api/client"
import { keys, useLabelCheck, useStatus } from "@/api/hooks"
import { StateBadge } from "@/components/state-badge"
import { ago } from "@/lib/format"
import { classify, OS_ARCH } from "@/lib/labels"
import { useNow } from "@/lib/use-now"
import { errorText } from "@/query"

export function LabelCheckCard({
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
      toast.error(`label check: ${errorText(err)}`)
    } finally {
      setStarting(false)
    }
  }

  let header = "Not checked yet"
  if (lc?.state === "checking") header = "Checking…"
  else if (lc?.state === "done" && lc.checked_at) header = `Checked ${ago(now - Date.parse(lc.checked_at))}${lc.partial ? " · partial" : ""}`

  const offered = new Set<string>()
  return (
    <Card>
      <CardHeader>
        <CardTitle>Workflow labels</CardTitle>
      </CardHeader>
      <CardContent className="flex flex-col gap-3 text-sm">
        <div className="flex items-center gap-3">
          <span>{header}</span>
          <Button size="sm" variant="outline" disabled={disabled || degraded || starting || lc?.state === "checking"} onClick={() => void start()}>
            Check now
          </Button>
        </div>
        {degraded && <p className="text-destructive">GitHub is rejecting the token: {degradedReason}</p>}
        {lc?.error && <p className="text-destructive">✖ {lc.error}</p>}
        {check.isError && !degraded && <p className="text-destructive">✖ {errorText(check.error)}</p>}
        <ul className="flex flex-col gap-3">
          {(lc?.groups ?? []).map((g, i) => {
            const { kind, missing } = classify(g.labels, effective)
            const adds = missing.filter((l) => !OS_ARCH.has(l) && !offered.has(l))
            for (const l of adds) offered.add(l)
            return (
              <li key={i} className="flex flex-col gap-1">
                <div className="flex flex-wrap items-center gap-2">
                  <StateBadge state={kind} />
                  <span>{g.labels.length > 0 ? g.labels.join(", ") : "no labels (runner group)"}</span>
                  <span>{g.more > 0 ? `${g.jobs.join(", ")} +${g.more} more` : g.jobs.join(", ")}</span>
                  <span className="text-muted-foreground">
                    · {g.count} jobs · {ago(now - Date.parse(g.last_seen))}
                  </span>
                </div>
                {kind === "unmatched" && (
                  <div className="flex flex-col gap-1 pl-4">
                    <p>missing {missing.join(", ")}</p>
                    {adds.length > 0 && (
                      <div className="flex flex-wrap gap-2">
                        {adds.map((l) => (
                          <Button key={l} size="sm" variant="secondary" disabled={disabled} onClick={() => onAddLabel(l)}>
                            + add {l}
                          </Button>
                        ))}
                      </div>
                    )}
                    {missing.some((l) => OS_ARCH.has(l)) && <p className="text-amber-600">needs a different OS or architecture</p>}
                  </div>
                )}
              </li>
            )
          })}
        </ul>
        {offered.size > 0 && (
          <p className="text-xs text-muted-foreground">
            Adding a label changes which jobs ghr accepts; it does not install anything on the runner.
          </p>
        )}
      </CardContent>
    </Card>
  )
}
```

- [ ] **Step 5: Wire it into the repository page**

In `web/src/pages/repository.tsx`, add below the `import { ActivitySummary, RepoSummary } from "@/components/repo-summary"` line:

```tsx
import { LabelCheckCard } from "@/components/label-check-card"
```

Below `  const custom = customLabels(cfg.labels ?? [], values.labels as string[])`, add:

```tsx
  const effective = [...SYSTEM_LABELS, ...custom.map((c) => c.label)]
  const degraded = status.data?.degraded ?? false

  function addLabel(label: string) {
    const current = values.labels as string[]
    if (current.some((l) => l.toLowerCase() === label.toLowerCase())) return
    setDraft((d) => ({ ...d, labels: [...current, label] }))
  }
```

Replace the line `      <SaveBar count={changed.length} saving={saving} disabled={status.isError} onSave={() => void save()} onDiscard={discard} />` with:

```tsx
      <div className="mt-4 grid gap-4 lg:grid-cols-2">
        <LabelCheckCard name={name} effective={effective} degraded={degraded} degradedReason={status.data?.degraded_reason} disabled={locked} onAddLabel={addLabel} />
      </div>
      <SaveBar count={changed.length} saving={saving} disabled={status.isError} onSave={() => void save()} onDiscard={discard} />
```

- [ ] **Step 6: Run the checks**

Run: `timeout 300 npm --prefix web test -- src/lib/labels.test.ts src/components/label-check-card.test.tsx src/pages/repository.test.tsx`, then the four web checks.
Expected: all PASS. `repository.test.tsx` does not mock the label check; its 404 shows as a `✖` line and no test there asserts against it.

- [ ] **Step 7: Commit**

```bash
git add web/src/lib/labels.ts web/src/lib/labels.test.ts web/src/components/label-check-card.tsx web/src/components/label-check-card.test.tsx web/src/pages/repository.tsx
git commit -m "feat(web): add the workflow labels card"
```

### Task 10: web: GitHub registrations card

**Files:**
- Create: `web/src/components/registrations-card.tsx`
- Modify: `web/src/pages/repository.tsx` (from Task 9)
- Test: `web/src/components/registrations-card.test.tsx`

**Interfaces:**
- Consumes: C3 (`deleteRegistration`), C5 (`useRegistrations`, `keys`), C6 (`ConfirmDialog`), Task 9's `RepositoryPage` (`degraded`, `locked`, the `<LabelCheckCard … />` line); C1 (`useStatus`).
- Produces: `RegistrationsCard({ name, degraded, degradedReason, disabled }: { name: string; degraded: boolean; degradedReason?: string; disabled: boolean })`.

**Items:** 11, 14
**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 0 = 3

- [ ] **Step 1: Write the failing test**

Create `web/src/components/registrations-card.test.tsx`:

```tsx
import { screen, waitFor, within } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import type { Registration } from "@/api/types"
import { mockApi, noContent } from "@/test/api"
import { authedRoutes, fixtures } from "@/test/fixtures"
import { renderApp } from "@/test/render"

const stale: Registration = { id: 9, name: "old-runner", status: "offline", busy: false, labels: ["self-hosted", "linux"], ghr: false }

function routes(over: Record<string, unknown> = {}) {
  return authedRoutes({
    "GET /api/history": [],
    "GET /api/repos/darkmem/label-check": fixtures.labelCheck,
    "GET /api/repos/darkmem/registrations": [...fixtures.registrations, stale],
    ...over,
  })
}

describe("GitHub registrations card", () => {
  it("lists registrations and offers Delete only for an offline foreign one", async () => {
    mockApi(routes())
    renderApp("/repositories/darkmem")
    expect(await screen.findByText("ghr-aaaaaa")).toBeInTheDocument()
    expect(screen.getByText("old-runner")).toBeInTheDocument()
    expect(screen.getByText("self-hosted linux")).toBeInTheDocument()
    expect(screen.getByText("offline")).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Delete old-runner" })).toBeInTheDocument()
    expect(screen.queryByRole("button", { name: "Delete ghr-aaaaaa" })).toBeNull()
  })

  it("deletes a registration only after confirmation", async () => {
    const { calls } = mockApi(routes({ "DELETE /api/repos/darkmem/registrations/9": () => noContent() }))
    const { user } = renderApp("/repositories/darkmem")
    await user.click(await screen.findByRole("button", { name: "Delete old-runner" }))
    expect(await screen.findByText("Delete the runner registration old-runner from darkmem?")).toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: "Cancel" }))
    await waitFor(() => expect(screen.queryByRole("alertdialog")).toBeNull())
    expect(calls.some((c) => c.method === "DELETE")).toBe(false)

    await user.click(screen.getByRole("button", { name: "Delete old-runner" }))
    await user.click(within(await screen.findByRole("alertdialog")).getByRole("button", { name: "Delete" }))
    await waitFor(() =>
      expect(calls.some((c) => c.method === "DELETE" && c.path === "/api/repos/darkmem/registrations/9")).toBe(true),
    )
    expect((await screen.findAllByText("deleted old-runner")).length).toBeGreaterThan(0)
  })

  it("says when nothing is registered, and refreshes", async () => {
    const { calls } = mockApi(routes({ "GET /api/repos/darkmem/registrations": [] }))
    const { user } = renderApp("/repositories/darkmem")
    expect(
      await screen.findByText("No runners registered. ghr starts single-use runners on demand (and keeps warm ones in all mode)."),
    ).toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: "Refresh" }))
    await waitFor(() => expect(calls.filter((c) => c.path === "/api/repos/darkmem/registrations")).toHaveLength(2))
  })

  it("does not read while GitHub rejects the token", async () => {
    const { calls } = mockApi(
      routes({ "GET /api/status": { ...fixtures.status, degraded: true, degraded_reason: "bad credentials" } }),
    )
    renderApp("/repositories/darkmem")
    expect((await screen.findAllByText("GitHub is rejecting the token: bad credentials")).length).toBe(2)
    expect(calls.some((c) => c.path.endsWith("/registrations"))).toBe(false)
  })
})
```

- [ ] **Step 2: Run it to see it fail**

Run: `timeout 300 npm --prefix web test -- src/components/registrations-card.test.tsx`
Expected: FAIL; `ghr-aaaaaa` is not on the page.

- [ ] **Step 3: Implement the card**

Create `web/src/components/registrations-card.tsx`:

```tsx
import { useMutation, useQueryClient } from "@tanstack/react-query"
import { Badge } from "darkraise-ui/components/badge"
import { Button } from "darkraise-ui/components/button"
import { Card, CardContent, CardHeader, CardTitle } from "darkraise-ui/components/card"
import { toast } from "darkraise-ui/components/sonner"
import { useState } from "react"
import { api } from "@/api/client"
import { keys, useRegistrations, useStatus } from "@/api/hooks"
import type { Registration } from "@/api/types"
import { ConfirmDialog, type Confirm } from "@/components/confirm-dialog"
import { StateBadge } from "@/components/state-badge"
import { errorText } from "@/query"

export function RegistrationsCard({
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
      toast.success(`deleted ${r.name}`)
    },
    onSettled: () => queryClient.invalidateQueries({ queryKey: keys.registrations(name) }),
  })

  let body
  if (degraded) body = <p className="text-destructive">GitHub is rejecting the token: {degradedReason}</p>
  else if (regs.isError) body = <p className="text-destructive">✖ {errorText(regs.error)}</p>
  else if (!regs.data) body = <p className="text-muted-foreground">loading…</p>
  else if (regs.data.length === 0) {
    body = (
      <p className="text-muted-foreground">
        No runners registered. ghr starts single-use runners on demand (and keeps warm ones in all mode).
      </p>
    )
  } else {
    body = (
      <ul className="flex flex-col gap-2">
        {regs.data.map((r) => (
          <li key={r.id} className="flex flex-wrap items-center gap-2">
            <StateBadge state={r.busy ? "busy" : r.status} />
            {r.ghr && (
              <Badge variant="secondary" size="sm">
                ghr
              </Badge>
            )}
            <span>{r.name}</span>
            <span className="text-muted-foreground">{r.labels.join(" ")}</span>
            {!r.ghr && !r.busy && r.status === "offline" && (
              <Button
                size="sm"
                variant="destructive"
                aria-label={`Delete ${r.name}`}
                disabled={disabled}
                onClick={() =>
                  setConfirm({
                    title: `Delete the runner registration ${r.name} from ${name}?`,
                    action: "Delete",
                    destructive: true,
                    run: () => remove.mutate(r),
                  })
                }
              >
                Delete
              </Button>
            )}
          </li>
        ))}
      </ul>
    )
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>GitHub registrations</CardTitle>
      </CardHeader>
      <CardContent className="flex flex-col gap-3 text-sm">
        {body}
        <div>
          <Button size="sm" variant="outline" disabled={disabled || degraded} onClick={() => void regs.refetch()}>
            Refresh
          </Button>
        </div>
      </CardContent>
      <ConfirmDialog confirm={confirm} onClose={() => setConfirm(null)} />
    </Card>
  )
}
```

- [ ] **Step 4: Wire it into the repository page**

In `web/src/pages/repository.tsx`, add below the `LabelCheckCard` import:

```tsx
import { RegistrationsCard } from "@/components/registrations-card"
```

Replace the line `        <LabelCheckCard name={name} effective={effective} degraded={degraded} degradedReason={status.data?.degraded_reason} disabled={locked} onAddLabel={addLabel} />` with:

```tsx
        <LabelCheckCard name={name} effective={effective} degraded={degraded} degradedReason={status.data?.degraded_reason} disabled={locked} onAddLabel={addLabel} />
        <RegistrationsCard name={name} degraded={degraded} degradedReason={status.data?.degraded_reason} disabled={locked} />
```

- [ ] **Step 5: Run the checks**

Run: `timeout 300 npm --prefix web test -- src/components/registrations-card.test.tsx src/components/label-check-card.test.tsx src/pages/repository.test.tsx`, then the four web checks.
Expected: all PASS. The degraded test expects the token line twice: once in each card.

- [ ] **Step 6: Commit**

```bash
git add web/src/components/registrations-card.tsx web/src/components/registrations-card.test.tsx web/src/pages/repository.tsx
git commit -m "feat(web): add the GitHub registrations card"
```

### Task 11: web: Storage page and Docker disk

**Files:**
- Create: `web/src/components/refused-hint.tsx`
- Modify: `web/src/pages/storage.tsx` (replace the placeholder), `web/src/router.test.tsx` (one path)
- Test: `web/src/pages/storage.test.tsx`

**Interfaces:**
- Consumes: C3 (`api.prune`), C5 (`useStorage`, `keys.storage`), C6 (`ConfirmDialog`, `Confirm`), C1 (`useStatus`, `useConfig`, `humanBytes`, `hhmm`, `errorText`).
- Produces: `web/src/components/refused-hint.tsx` exports `busyJobs(status: Status | undefined): number` (instances in state `busy`) and `RefusedHint({ what, status }: { what: string; status: Status | undefined })`, which renders `<what>: refused while <n> jobs run` while any runner is busy and nothing otherwise. `StoragePage` lays its cards out in a grid inside `<div className="grid gap-4 xl:grid-cols-2">`, in the order Docker disk, Toolchains, Package caches, Recent operations. Tasks 12 and 14 add the later cards to that grid.

**Items:** 12, 14
**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 1 = 4

- [ ] **Step 1: Write the failing test**

Create `web/src/pages/storage.test.tsx`:

```tsx
import { screen, waitFor, within } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import { json, mockApi, noContent } from "@/test/api"
import { authedRoutes, fixtures } from "@/test/fixtures"
import { renderApp } from "@/test/render"

const routes = (over: Record<string, unknown> = {}) => authedRoutes({ "GET /api/storage": fixtures.storage, ...over })

describe("Storage page", () => {
  it("shows Docker's disk use, its rows and the last prune", async () => {
    mockApi(routes())
    renderApp("/storage")
    expect(await screen.findByText("61% used · prunes above 80%")).toBeInTheDocument()
    expect(screen.getByText("8.1 GB")).toBeInTheDocument()
    expect(screen.getByText("regular")).toBeInTheDocument()
    expect(screen.getByText(/^manual build-cache-all · 13:16 · ok/)).toHaveTextContent(
      "manual build-cache-all · 13:16 · ok — all build cache 6.8 MB",
    )
    expect(screen.getByRole("button", { name: "Build cache to 20GB" })).toBeEnabled()
    expect(screen.getByText("Unused volumes: refused while 1 jobs run")).toBeInTheDocument()
  })

  it(
    "disables pruning while the daemon is unreachable",
    async () => {
      let reads = 0
      mockApi(routes({ "GET /api/status": () => (++reads === 1 ? fixtures.status : json({ error: "connection refused" }, 502)) }))
      renderApp("/storage")
      expect(await screen.findByText("Reconnecting", {}, { timeout: 4000 })).toBeInTheDocument()
      expect(screen.getByRole("button", { name: "Prune" })).toBeDisabled()
    },
    { timeout: 10_000 },
  )

  it("says when nothing was pruned since start", async () => {
    mockApi(routes({ "GET /api/storage": { ...fixtures.storage, last_prune: null } }))
    renderApp("/storage")
    expect(await screen.findByText("no prune since start")).toBeInTheDocument()
  })

  it("disables pruning while a prune runs", async () => {
    mockApi(routes({ "GET /api/status": { ...fixtures.status, maintenance: { running: true } } }))
    renderApp("/storage")
    expect(await screen.findByText("pruning…")).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Prune" })).toBeDisabled()
  })

  it("shows a failed read", async () => {
    mockApi(routes({ "GET /api/storage": () => json({ error: "storage unavailable" }, 500) }))
    renderApp("/storage")
    expect(await screen.findByText("✖ storage unavailable")).toBeInTheDocument()
    expect(screen.queryByText("loading…")).toBeNull()
  })

  it.each([
    ["Prune", "Prune now? Removes build cache beyond 20GB, dangling images, and history and logs past retention."],
    ["Build cache to 20GB", "Prune the build cache down to 20GB?"],
    ["All build cache", "Remove all build cache (up to 380.0 MB)? The next builds start cold."],
    ["Dangling images", "Remove dangling images (untagged and used by no container)?"],
    ["Unused volumes", "Remove every volume no container uses? It is refused while jobs run."],
  ])("asks before %s and sends nothing on Cancel", async (label, question) => {
    const { calls } = mockApi(routes())
    const { user } = renderApp("/storage")
    await user.click(await screen.findByRole("button", { name: label }))
    expect(await screen.findByText(question)).toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: "Cancel" }))
    await waitFor(() => expect(screen.queryByRole("alertdialog")).toBeNull())
    expect(calls.some((c) => c.method === "POST")).toBe(false)
  })

  it("prunes a scope once confirmed", async () => {
    const { calls } = mockApi(routes({ "POST /api/prune/build-cache-all": () => noContent() }))
    const { user } = renderApp("/storage")
    await user.click(await screen.findByRole("button", { name: "All build cache" }))
    await user.click(within(await screen.findByRole("alertdialog")).getByRole("button", { name: "All build cache" }))
    await waitFor(() => expect(calls.some((c) => c.method === "POST" && c.path === "/api/prune/build-cache-all")).toBe(true))
    expect((await screen.findAllByText("build-cache-all prune started")).length).toBeGreaterThan(0)
  })

  it("sends the standard prune to /prune", async () => {
    const { calls } = mockApi(routes({ "POST /api/prune": () => noContent() }))
    const { user } = renderApp("/storage")
    await user.click(await screen.findByRole("button", { name: "Prune" }))
    await user.click(within(await screen.findByRole("alertdialog")).getByRole("button", { name: "Prune" }))
    await waitFor(() => expect(calls.some((c) => c.method === "POST" && c.path === "/api/prune")).toBe(true))
    expect((await screen.findAllByText("standard prune started")).length).toBeGreaterThan(0)
  })
})
```

In `web/src/router.test.tsx`, the test `shows the later pages as not in the web UI yet` renders `/storage`, which this task replaces. Change its one line `renderAt("/storage")` to `renderAt("/settings")`.

- [ ] **Step 2: Run it to see it fail**

Run: `timeout 300 npm --prefix web test -- src/pages/storage.test.tsx`
Expected: FAIL, `61% used · prunes above 80%` is not found (the page is still the placeholder).

- [ ] **Step 3: Implement the busy hint**

Create `web/src/components/refused-hint.tsx`:

```tsx
import type { Status } from "@/api/types"

export function busyJobs(status: Status | undefined): number {
  return status?.instances.filter((i) => i.state === "busy").length ?? 0
}

export function RefusedHint({ what, status }: { what: string; status: Status | undefined }) {
  const n = busyJobs(status)
  if (n === 0) return null
  return (
    <p className="text-sm text-amber-600">
      {what}: refused while {n} jobs run
    </p>
  )
}
```

- [ ] **Step 4: Implement the page and the Docker disk card**

Replace `web/src/pages/storage.tsx` with:

```tsx
import { useMutation, useQueryClient } from "@tanstack/react-query"
import { Button } from "darkraise-ui/components/button"
import { Card, CardContent, CardHeader, CardTitle } from "darkraise-ui/components/card"
import { Progress } from "darkraise-ui/components/progress"
import { toast } from "darkraise-ui/components/sonner"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "darkraise-ui/components/table"
import { PageHeader } from "darkraise-ui/layout"
import { useState } from "react"
import { api } from "@/api/client"
import { keys, useConfig, useStatus, useStorage } from "@/api/hooks"
import type { Config, DockerDisk, PruneScope, Status, Storage } from "@/api/types"
import { ConfirmDialog, type Confirm } from "@/components/confirm-dialog"
import { RefusedHint } from "@/components/refused-hint"
import { hhmm, humanBytes } from "@/lib/format"
import { errorText } from "@/query"

function reclaim(d: DockerDisk, type: string): string {
  const row = (d.rows ?? []).find((r) => r.type === type && r.reclaimable > 0)
  return row ? ` (up to ${humanBytes(row.reclaimable)})` : ""
}

interface PruneButton {
  scope: PruneScope
  label: string
  variant: "secondary" | "destructive" | undefined
  question: string
}

function pruneButtons(storage: Storage, config: Config | undefined): PruneButton[] {
  const keep = config?.build_cache_keep
  const size = keep ?? "the build_cache_keep size"
  const d = storage.docker
  return [
    {
      scope: "standard",
      label: "Prune",
      variant: undefined,
      question: `Prune now? Removes build cache beyond ${size}, dangling images, and history and logs past retention.`,
    },
    { scope: "build-cache-keep", label: `Build cache to ${keep ?? "keep"}`, variant: "secondary", question: `Prune the build cache down to ${size}?` },
    {
      scope: "build-cache-all",
      label: "All build cache",
      variant: "destructive",
      question: `Remove all build cache${reclaim(d, "Build Cache")}? The next builds start cold.`,
    },
    { scope: "dangling-images", label: "Dangling images", variant: "secondary", question: "Remove dangling images (untagged and used by no container)?" },
    {
      scope: "unused-volumes",
      label: "Unused volumes",
      variant: "destructive",
      question: `Remove every volume no container uses${reclaim(d, "Local Volumes")}? It is refused while jobs run.`,
    },
  ]
}

function LastPrune({ storage, status }: { storage: Storage; status: Status | undefined }) {
  const p = storage.last_prune
  if (status?.maintenance.running || (p && !p.finished_at)) return <p className="text-sm text-amber-600">pruning…</p>
  if (!p?.finished_at) return <p className="text-sm text-muted-foreground">no prune since start</p>
  const head = p.trigger === "auto" ? p.trigger : `${p.trigger} ${p.scope}`
  const steps = p.steps ?? []
  return (
    <p className="text-sm">
      {`${head} · ${hhmm(new Date(p.finished_at))} · ${p.outcome ?? ""}`}
      {steps.length > 0 && " — "}
      {steps.map((st, i) => (
        <span key={st.name} className={st.error ? "text-destructive" : undefined}>
          {i > 0 && ", "}
          {st.error ? `${st.name}: ${st.error}` : `${st.name} ${humanBytes(st.freed)}`}
        </span>
      ))}
    </p>
  )
}

function DockerCard({
  storage,
  status,
  config,
  offline,
}: {
  storage: Storage
  status: Status | undefined
  config: Config | undefined
  offline: boolean
}) {
  const queryClient = useQueryClient()
  const [confirm, setConfirm] = useState<Confirm | null>(null)
  const prune = useMutation({
    mutationFn: (scope: PruneScope) => api.prune(scope),
    onSuccess: (_data, scope) => {
      toast.success(`${scope} prune started`)
    },
    onSettled: () =>
      Promise.all([
        queryClient.invalidateQueries({ queryKey: keys.storage }),
        queryClient.invalidateQueries({ queryKey: keys.status }),
      ]),
  })
  const d = storage.docker
  const disabled = offline || (status?.maintenance.running ?? false)
  return (
    <Card>
      <CardHeader>
        <CardTitle>Docker disk</CardTitle>
      </CardHeader>
      <CardContent className="flex flex-col gap-3">
        <Progress value={d.disk_pct} aria-label="disk use" />
        <p className="text-sm">
          {d.disk_pct}% used · prunes above {config?.disk_high_water ?? 80}%
        </p>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead />
              <TableHead>count</TableHead>
              <TableHead>active</TableHead>
              <TableHead>size</TableHead>
              <TableHead>reclaimable</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {(d.rows ?? []).flatMap((r) => [
              <TableRow key={r.type}>
                <TableCell>{r.type}</TableCell>
                <TableCell>{r.count}</TableCell>
                <TableCell>{r.active}</TableCell>
                <TableCell>{humanBytes(r.bytes)}</TableCell>
                <TableCell>{humanBytes(r.reclaimable)}</TableCell>
              </TableRow>,
              ...(r.type === "Build Cache" ? (d.build_cache_types ?? []) : []).map((t) => (
                <TableRow key={`build-cache-${t.type}`} className="text-muted-foreground">
                  <TableCell className="pl-6">{t.type}</TableCell>
                  <TableCell>{t.count}</TableCell>
                  <TableCell />
                  <TableCell>{humanBytes(t.bytes)}</TableCell>
                  <TableCell>{humanBytes(t.reclaimable)}</TableCell>
                </TableRow>
              )),
            ])}
          </TableBody>
        </Table>
        <LastPrune storage={storage} status={status} />
        <div className="flex flex-wrap gap-2">
          {pruneButtons(storage, config).map((b) => (
            <Button
              key={b.scope}
              size="sm"
              variant={b.variant}
              disabled={disabled}
              onClick={() =>
                setConfirm({ title: b.question, action: b.label, destructive: b.variant === "destructive", run: () => prune.mutate(b.scope) })
              }
            >
              {b.label}
            </Button>
          ))}
        </div>
        <RefusedHint what="Unused volumes" status={status} />
      </CardContent>
      <ConfirmDialog confirm={confirm} onClose={() => setConfirm(null)} />
    </Card>
  )
}

export function StoragePage() {
  const status = useStatus()
  const config = useConfig()
  const storage = useStorage(status.data)
  const offline = status.isError
  const data = storage.data
  return (
    <>
      <PageHeader title="Storage" />
      {storage.isError && <p className="mb-4 text-sm text-destructive">✖ {errorText(storage.error)}</p>}
      {data ? (
        <div className="grid gap-4 xl:grid-cols-2">
          <DockerCard storage={data} status={status.data} config={config.data} offline={offline} />
        </div>
      ) : (
        !storage.isError && <p className="text-sm text-muted-foreground">loading…</p>
      )}
    </>
  )
}
```

- [ ] **Step 5: Run the checks**

Run: `timeout 300 npm --prefix web test -- src/pages/storage.test.tsx src/router.test.tsx`, then the four web checks.
Expected: all PASS. The times assume vitest's `TZ=UTC` (part 2, C2): the last prune finished at 13:16 UTC.

- [ ] **Step 6: Commit**

```bash
git add web/src/components/refused-hint.tsx web/src/pages/storage.tsx web/src/pages/storage.test.tsx web/src/router.test.tsx
git commit -m "feat(web): add the Storage page and Docker disk"
```

### Task 12: web: Toolchains card

**Files:**
- Create: `web/src/components/toolchains-card.tsx`
- Modify: `web/src/pages/storage.tsx`
- Test: `web/src/components/toolchains-card.test.tsx`

**Interfaces:**
- Consumes: C3 (`api.removeToolchain`, `api.installPreset`), C5 (`keys.storage`), C6, Task 11 (`RefusedHint`, the Storage grid).
- Produces: `web/src/components/toolchains-card.tsx` exports `ToolchainsCard({ storage, status, offline }: { storage: Storage; status: Status | undefined; offline: boolean })`, `lastDotnetMajor(t: Toolchain, all: Toolchain[]): string` and `queueText(ops: Operations): string`. Task 13 adds the `Install…` button to its button row, next to `Install popular set`.

**Items:** 12, 14
**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 1 = 4

- [ ] **Step 1: Write the failing test**

Create `web/src/components/toolchains-card.test.tsx`:

```tsx
import { screen, waitFor, within } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import type { Toolchain } from "@/api/types"
import { mockApi, noContent } from "@/test/api"
import { authedRoutes, fixtures } from "@/test/fixtures"
import { renderApp } from "@/test/render"
import { lastDotnetMajor, queueText } from "./toolchains-card"

const routes = (over: Record<string, unknown> = {}) => authedRoutes({ "GET /api/storage": fixtures.storage, ...over })

const sdk = (version: string): Toolchain => ({ tool: "dotnet", version, arch: "x64", path: "", bytes: 1, installed_at: "2026-10-01T00:00:00Z" })

describe("lastDotnetMajor", () => {
  it("names the major only for the last SDK of that major", () => {
    const all = [sdk("8.0.404"), sdk("8.0.100"), sdk("10.0.100")]
    expect(lastDotnetMajor(sdk("10.0.100"), all)).toBe("10")
    expect(lastDotnetMajor(sdk("8.0.404"), all)).toBe("")
    expect(lastDotnetMajor({ ...sdk("22.11.0"), tool: "node" }, all)).toBe("")
  })
})

describe("queueText", () => {
  it("describes the running operation and the queue", () => {
    expect(queueText(fixtures.storage.operations)).toBe("installing node 24 — extracting (1 queued)")
    expect(queueText({ current: null, queued: 2, recent: [] })).toBe("2 queued")
    expect(queueText({ current: null, queued: 0, recent: [] })).toBe("")
    expect(queueText({ current: { id: "x", kind: "clear", target: "nuget", started_at: "" }, queued: 0, recent: [] })).toBe("clearing nuget")
  })
})

describe("Toolchains card", () => {
  it("lists toolchains, other folders and the queue", async () => {
    mockApi(routes())
    renderApp("/storage")
    expect(await screen.findByText("node 22.11.0")).toBeInTheDocument()
    expect(screen.getByText("190.0 MB")).toBeInTheDocument()
    expect(screen.getByText("installed 2026-09-30")).toBeInTheDocument()
    expect(screen.getByText("PyPy")).toBeInTheDocument()
    expect(screen.getByText("other: a job's own setup step")).toBeInTheDocument()
    expect(screen.getByText("installing node 24 — extracting (1 queued)")).toBeInTheDocument()
    expect(screen.getByText("Remove: refused while 1 jobs run")).toBeInTheDocument()
  })

  it("says when the tool cache is empty", async () => {
    mockApi(routes({ "GET /api/storage": { ...fixtures.storage, toolchains: null, other_tool_cache: [] } }))
    renderApp("/storage")
    expect(await screen.findByText("the tool cache is empty")).toBeInTheDocument()
  })

  it("removes a toolchain only once confirmed", async () => {
    const { calls } = mockApi(routes({ "DELETE /api/toolchains/node/22.11.0": () => noContent() }))
    const { user } = renderApp("/storage")
    await user.click(await screen.findByRole("button", { name: "Remove node 22.11.0" }))
    expect(await screen.findByText("Remove node 22.11.0 (190.0 MB)?")).toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: "Cancel" }))
    await waitFor(() => expect(screen.queryByRole("alertdialog")).toBeNull())
    expect(calls.some((c) => c.method === "DELETE")).toBe(false)

    await user.click(screen.getByRole("button", { name: "Remove node 22.11.0" }))
    await user.click(within(await screen.findByRole("alertdialog")).getByRole("button", { name: "Remove" }))
    await waitFor(() => expect(calls.some((c) => c.method === "DELETE" && c.path === "/api/toolchains/node/22.11.0")).toBe(true))
    expect((await screen.findAllByText("queued: remove node 22.11.0")).length).toBeGreaterThan(0)
  })

  it("warns when removing the last .NET SDK of a major", async () => {
    const storage = { ...fixtures.storage, toolchains: [sdk("8.0.404")] }
    mockApi(routes({ "GET /api/storage": storage }))
    const { user } = renderApp("/storage")
    await user.click(await screen.findByRole("button", { name: "Remove dotnet 8.0.404" }))
    expect(
      await screen.findByText("Remove dotnet 8.0.404 (1 B)? It is the last .NET 8 SDK, so this also removes the 8.0 runtimes and packs."),
    ).toBeInTheDocument()
  })

  it("installs the popular set only once confirmed", async () => {
    const { calls } = mockApi(routes({ "POST /api/toolchains": () => noContent() }))
    const { user } = renderApp("/storage")
    await user.click(await screen.findByRole("button", { name: "Install popular set" }))
    const question =
      "Install the popular set? node 22, node 24, dotnet 8.0, dotnet 10.0, python 3.13, python 3.14, go latest, java 21, java 25. Versions already installed are skipped."
    expect(await screen.findByText(question)).toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: "Cancel" }))
    await waitFor(() => expect(screen.queryByRole("alertdialog")).toBeNull())
    expect(calls.some((c) => c.method === "POST")).toBe(false)

    await user.click(screen.getByRole("button", { name: "Install popular set" }))
    await user.click(within(await screen.findByRole("alertdialog")).getByRole("button", { name: "Install popular set" }))
    await waitFor(() => expect(calls.find((c) => c.method === "POST")?.body).toEqual({ preset: "popular" }))
    expect((await screen.findAllByText("queued: popular set")).length).toBeGreaterThan(0)
  })
})
```

- [ ] **Step 2: Run it to see it fail**

Run: `timeout 300 npm --prefix web test -- src/components/toolchains-card.test.tsx`
Expected: FAIL, cannot resolve `./toolchains-card`.

- [ ] **Step 3: Implement the card**

Create `web/src/components/toolchains-card.tsx`:

```tsx
import { useMutation, useQueryClient } from "@tanstack/react-query"
import { Button } from "darkraise-ui/components/button"
import { Card, CardContent, CardHeader, CardTitle } from "darkraise-ui/components/card"
import { toast } from "darkraise-ui/components/sonner"
import { Table, TableBody, TableCell, TableRow } from "darkraise-ui/components/table"
import { useState } from "react"
import { api } from "@/api/client"
import { keys } from "@/api/hooks"
import type { Operations, Status, Storage, Toolchain } from "@/api/types"
import { ConfirmDialog, type Confirm } from "@/components/confirm-dialog"
import { RefusedHint } from "@/components/refused-hint"
import { dateTime, humanBytes } from "@/lib/format"

// The daemon's popular preset (internal/toolchain/set.go); the TUI lists it
// in the same words before queueing it.
const POPULAR_QUESTION =
  "Install the popular set? node 22, node 24, dotnet 8.0, dotnet 10.0, python 3.13, python 3.14, go latest, java 21, java 25. Versions already installed are skipped."

const VERBS: Record<string, string> = { install: "installing", remove: "removing", clear: "clearing" }

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
  let text = `${VERBS[c.kind] ?? c.kind} ${c.target}`
  if (c.progress) text += ` — ${c.progress}`
  if (ops.queued > 0) text += ` (${ops.queued} queued)`
  return text
}

export function ToolchainsCard({ storage, status, offline }: { storage: Storage; status: Status | undefined; offline: boolean }) {
  const queryClient = useQueryClient()
  const [confirm, setConfirm] = useState<Confirm | null>(null)
  const settled = () => queryClient.invalidateQueries({ queryKey: keys.storage })
  const remove = useMutation({
    mutationFn: (t: Toolchain) => api.removeToolchain(t.tool, t.version),
    onSuccess: (_data, t) => {
      toast.success(`queued: remove ${t.tool} ${t.version}`)
    },
    onSettled: settled,
  })
  const popular = useMutation({
    mutationFn: () => api.installPreset("popular"),
    onSuccess: () => {
      toast.success("queued: popular set")
    },
    onSettled: settled,
  })
  const tools = storage.toolchains ?? []
  const others = storage.other_tool_cache ?? []
  const queue = queueText(storage.operations)

  function askRemove(t: Toolchain) {
    const major = lastDotnetMajor(t, tools)
    let title = `Remove ${t.tool} ${t.version} (${humanBytes(t.bytes)})?`
    if (major) title += ` It is the last .NET ${major} SDK, so this also removes the ${major}.0 runtimes and packs.`
    setConfirm({ title, action: "Remove", destructive: true, run: () => remove.mutate(t) })
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>Toolchains</CardTitle>
      </CardHeader>
      <CardContent className="flex flex-col gap-3">
        {tools.length === 0 && others.length === 0 ? (
          <p className="text-sm text-muted-foreground">the tool cache is empty</p>
        ) : (
          <Table>
            <TableBody>
              {tools.map((t) => (
                <TableRow key={`${t.tool}/${t.version}/${t.arch}`}>
                  <TableCell>{`${t.tool} ${t.version}`}</TableCell>
                  <TableCell>{humanBytes(t.bytes)}</TableCell>
                  <TableCell className="text-muted-foreground">{`installed ${dateTime(t.installed_at).slice(0, 10)}`}</TableCell>
                  <TableCell className="text-right">
                    <Button
                      size="sm"
                      variant="destructive"
                      aria-label={`Remove ${t.tool} ${t.version}`}
                      disabled={offline}
                      onClick={() => askRemove(t)}
                    >
                      Remove
                    </Button>
                  </TableCell>
                </TableRow>
              ))}
              {others.map((f) => (
                <TableRow key={`other/${f.name}`}>
                  <TableCell>{f.name}</TableCell>
                  <TableCell>{humanBytes(f.bytes)}</TableCell>
                  <TableCell colSpan={2} className="text-muted-foreground">
                    {"other: a job's own setup step"}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
        {queue && <p className="text-sm text-amber-600">{queue}</p>}
        <RefusedHint what="Remove" status={status} />
        <div className="flex flex-wrap gap-2">
          <Button
            variant="secondary"
            disabled={offline}
            onClick={() => setConfirm({ title: POPULAR_QUESTION, action: "Install popular set", run: () => popular.mutate() })}
          >
            Install popular set
          </Button>
        </div>
      </CardContent>
      <ConfirmDialog confirm={confirm} onClose={() => setConfirm(null)} />
    </Card>
  )
}
```

- [ ] **Step 4: Put the card on the page**

In `web/src/pages/storage.tsx`, add the import after the `RefusedHint` import:

```tsx
import { ToolchainsCard } from "@/components/toolchains-card"
```

and, inside the grid, after the `<DockerCard … />` line:

```tsx
          <ToolchainsCard storage={data} status={status.data} offline={offline} />
```

- [ ] **Step 5: Run the checks**

Run: `timeout 300 npm --prefix web test -- src/components/toolchains-card.test.tsx src/pages/storage.test.tsx`, then the four web checks.
Expected: all PASS. Lint may warn (react-refresh) that `toolchains-card.tsx` exports functions besides the component; warnings are allowed.

- [ ] **Step 6: Commit**

```bash
git add web/src/components/toolchains-card.tsx web/src/components/toolchains-card.test.tsx web/src/pages/storage.tsx
git commit -m "feat(web): add the Toolchains card"
```

### Task 13: web: Install toolchain dialog

**Files:**
- Create: `web/src/components/install-dialog.tsx`
- Modify: `web/src/components/toolchains-card.tsx`
- Test: `web/src/components/install-dialog.test.tsx`

**Interfaces:**
- Consumes: C3 (`api.installToolchain`), C5 (`useToolchainChoices`, `keys.storage`), C1 (`useStatus`, `errorText`), Task 12 (`ToolchainsCard`'s button row).
- Produces: `web/src/components/install-dialog.tsx` exports `InstallDialog({ open, onClose }: { open: boolean; onClose: () => void })`.

**Items:** 12
**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 0 = 3

- [ ] **Step 1: Write the failing test**

Create `web/src/components/install-dialog.test.tsx`:

```tsx
import { screen, waitFor, within } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import { json, mockApi, noContent } from "@/test/api"
import { authedRoutes, fixtures } from "@/test/fixtures"
import { renderApp } from "@/test/render"

const goChoices = [{ spec: "1.23.4", version: "1.23.4" }]

const routes = (over: Record<string, unknown> = {}) =>
  authedRoutes({
    "GET /api/storage": fixtures.storage,
    "GET /api/toolchains/available": ({ url }: { url: URL }) =>
      url.searchParams.get("tool") === "node" ? fixtures.toolchainChoices : goChoices,
    ...over,
  })

async function openDialog(user: ReturnType<typeof renderApp>["user"]) {
  await user.click(await screen.findByRole("button", { name: "Install…" }))
  return within(await screen.findByRole("dialog"))
}

describe("Install toolchain dialog", () => {
  it("lists a tool's versions and installs the picked one", async () => {
    const { calls } = mockApi(routes({ "POST /api/toolchains": () => noContent() }))
    const { user } = renderApp("/storage")
    const dialog = await openDialog(user)
    expect(dialog.getByText("Install toolchain")).toBeInTheDocument()
    expect(dialog.getByText("pick one below, or type a version")).toBeInTheDocument()
    expect(await dialog.findByText("lts")).toBeInTheDocument()
    await user.click(dialog.getByRole("button", { name: /^22\.11\.0/ }))
    await user.click(dialog.getByRole("button", { name: "Install" }))
    await waitFor(() =>
      expect(calls.find((c) => c.method === "POST" && c.path === "/api/toolchains")?.body).toEqual({ tool: "node", version: "22.11.0" }),
    )
    expect((await screen.findAllByText("queued: install node 22.11.0")).length).toBeGreaterThan(0)
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull())
  })

  it("switches the tool and installs a typed version", async () => {
    const { calls } = mockApi(routes({ "POST /api/toolchains": () => noContent() }))
    const { user } = renderApp("/storage")
    const dialog = await openDialog(user)
    await user.click(dialog.getByRole("combobox", { name: "Tool" }))
    await user.click(await screen.findByRole("option", { name: "Go" }))
    expect(await dialog.findByRole("button", { name: /^1\.23\.4/ })).toBeInTheDocument()
    expect(calls.some((c) => c.path === "/api/toolchains/available" && c.search === "?tool=go")).toBe(true)
    await user.type(dialog.getByRole("textbox", { name: "Version" }), "1.22")
    expect(dialog.getByText("no match")).toBeInTheDocument()
    await user.click(dialog.getByRole("button", { name: "Install" }))
    await waitFor(() =>
      expect(calls.find((c) => c.method === "POST" && c.path === "/api/toolchains")?.body).toEqual({ tool: "go", version: "1.22" }),
    )
  })

  it("offers a retry when the versions cannot be read", async () => {
    mockApi(routes({ "GET /api/toolchains/available": () => json({ error: "upstream down" }, 502) }))
    const { user } = renderApp("/storage")
    const dialog = await openDialog(user)
    expect(await dialog.findByText("✖ upstream down")).toBeInTheDocument()
    expect(dialog.getByRole("button", { name: "Retry" })).toBeInTheDocument()
    expect(dialog.getByRole("button", { name: "Install" })).toBeDisabled()
  })

  it("keeps a rejected install in the dialog", async () => {
    mockApi(routes({ "POST /api/toolchains": () => json({ error: "unknown toolchain \"node\"" }, 400) }))
    const { user } = renderApp("/storage")
    const dialog = await openDialog(user)
    await user.type(dialog.getByRole("textbox", { name: "Version" }), "24")
    await user.click(dialog.getByRole("button", { name: "Install" }))
    expect(await dialog.findByText('✖ unknown toolchain "node"')).toBeInTheDocument()
    expect(screen.getByRole("dialog")).toBeInTheDocument()
  })
})
```

- [ ] **Step 2: Run it to see it fail**

Run: `timeout 300 npm --prefix web test -- src/components/install-dialog.test.tsx`
Expected: FAIL, no button named `Install…`.

- [ ] **Step 3: Implement the dialog**

Create `web/src/components/install-dialog.tsx`:

```tsx
import { useQueryClient } from "@tanstack/react-query"
import { Badge } from "darkraise-ui/components/badge"
import { Button } from "darkraise-ui/components/button"
import { Dialog, DialogBody, DialogContent, DialogFooter, DialogHeader, DialogTitle } from "darkraise-ui/components/dialog"
import { Input } from "darkraise-ui/components/input"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "darkraise-ui/components/select"
import { toast } from "darkraise-ui/components/sonner"
import { useState } from "react"
import { api } from "@/api/client"
import { keys, useStatus, useToolchainChoices } from "@/api/hooks"
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
  const offline = useStatus().isError
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
      toast.success(`queued: install ${tool} ${target}`)
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
          {target ? <strong>{target}</strong> : <span className="text-muted-foreground">pick one below, or type a version</span>}
        </p>
        <Input
          aria-label="Version"
          placeholder="type to filter"
          value={filter}
          onChange={(e) => {
            setFilter(e.target.value)
            setPicked(null)
          }}
        />
        {choices.isPending ? (
          <p className="text-sm text-muted-foreground">loading versions…</p>
        ) : choices.isError ? (
          <div className="flex items-center gap-2">
            <p className="text-sm text-destructive">✖ {errorText(choices.error)}</p>
            <Button size="sm" variant="outline" onClick={() => void choices.refetch()}>
              Retry
            </Button>
          </div>
        ) : shown.length === 0 ? (
          <p className="text-sm text-muted-foreground">{all.length === 0 ? "nothing to pick" : "no match"}</p>
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
                  {c.version}
                  {c.lts && (
                    <Badge variant="secondary" size="sm">
                      lts
                    </Badge>
                  )}
                </button>
              </li>
            ))}
          </ul>
        )}
        {error && <p className="text-sm text-destructive">✖ {error}</p>}
      </DialogBody>
      <DialogFooter>
        <Button variant="secondary" onClick={onClose}>
          Cancel
        </Button>
        <Button disabled={busy || offline || target === ""} onClick={() => void submit()}>
          {busy ? "Queuing…" : "Install"}
        </Button>
      </DialogFooter>
    </>
  )
}
```

- [ ] **Step 4: Add the Install button to the Toolchains card**

In `web/src/components/toolchains-card.tsx`, add the import after the `RefusedHint` import:

```tsx
import { InstallDialog } from "@/components/install-dialog"
```

Add a state hook after `const [confirm, setConfirm] = useState<Confirm | null>(null)`:

```tsx
  const [installing, setInstalling] = useState(false)
```

In the button row, before the `Install popular set` button, add:

```tsx
          <Button disabled={offline} onClick={() => setInstalling(true)}>
            Install…
          </Button>
```

and after `<ConfirmDialog confirm={confirm} onClose={() => setConfirm(null)} />` add:

```tsx
      <InstallDialog open={installing} onClose={() => setInstalling(false)} />
```

- [ ] **Step 5: Run the checks**

Run: `timeout 300 npm --prefix web test -- src/components/install-dialog.test.tsx src/components/toolchains-card.test.tsx`, then the four web checks.
Expected: all PASS. If `screen.findByRole("dialog")` finds nothing, read `node_modules/darkraise-ui/dist/components/dialog/useDialog.d.ts` for the role `DialogContent` renders, and query that role instead; record it in the commit body.

- [ ] **Step 6: Commit**

```bash
git add web/src/components/install-dialog.tsx web/src/components/install-dialog.test.tsx web/src/components/toolchains-card.tsx
git commit -m "feat(web): add the Install toolchain dialog"
```

### Task 14: web: package caches and recent operations

**Files:**
- Create: `web/src/components/storage-cards.tsx`
- Modify: `web/src/pages/storage.tsx`
- Test: `web/src/components/storage-cards.test.tsx`

**Interfaces:**
- Consumes: C3 (`api.clearCache`, `api.refreshStorage`), C5 (`keys.storage`), C6, C1 (`useNow`, `ago`, `hhmm`, `humanBytes`), Task 11 (`RefusedHint`, the Storage grid).
- Produces: `web/src/components/storage-cards.tsx` exports `PackageCachesCard({ storage, status, offline }: { storage: Storage; status: Status | undefined; offline: boolean })`, `OperationsCard({ storage }: { storage: Storage })` and `useOperationToasts(storage: Storage | undefined): void`.

**Items:** 12, 14
**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 1 = 4

- [ ] **Step 1: Write the failing test**

Create `web/src/components/storage-cards.test.tsx`:

```tsx
import { screen, waitFor, within } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import type { Storage } from "@/api/types"
import { mockApi, noContent } from "@/test/api"
import { authedRoutes, fixtures } from "@/test/fixtures"
import { renderApp } from "@/test/render"

const routes = (over: Record<string, unknown> = {}) => authedRoutes({ "GET /api/storage": fixtures.storage, ...over })

describe("Package caches card", () => {
  it("lists present and absent caches and when they were measured", async () => {
    mockApi(routes())
    renderApp("/storage")
    expect(await screen.findByText("41000 files")).toBeInTheDocument()
    expect(screen.getByText("3.6 GB")).toBeInTheDocument()
    expect(screen.getByText(/^written (just now|\d+[mhd] ago)$/)).toBeInTheDocument()
    expect(screen.getByText("not present")).toBeInTheDocument()
    expect(screen.queryByRole("button", { name: "Clear Cargo" })).toBeNull()
    expect(screen.getByText("measured 14:03")).toBeInTheDocument()
    expect(screen.getByText("Clear: refused while 1 jobs run")).toBeInTheDocument()
  })

  it("clears a cache only once confirmed", async () => {
    const { calls } = mockApi(routes({ "POST /api/caches/nuget/clear": () => noContent() }))
    const { user } = renderApp("/storage")
    await user.click(await screen.findByRole("button", { name: "Clear NuGet" }))
    expect(await screen.findByText("Clear the NuGet cache (3.6 GB)? Jobs download what they need again.")).toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: "Cancel" }))
    await waitFor(() => expect(screen.queryByRole("alertdialog")).toBeNull())
    expect(calls.some((c) => c.method === "POST")).toBe(false)

    await user.click(screen.getByRole("button", { name: "Clear NuGet" }))
    await user.click(within(await screen.findByRole("alertdialog")).getByRole("button", { name: "Clear" }))
    await waitFor(() => expect(calls.some((c) => c.method === "POST" && c.path === "/api/caches/nuget/clear")).toBe(true))
    expect((await screen.findAllByText("queued: clear NuGet")).length).toBeGreaterThan(0)
  })

  it("starts a measurement without asking", async () => {
    const { calls } = mockApi(routes({ "POST /api/storage/refresh": () => noContent() }))
    const { user } = renderApp("/storage")
    await user.click(await screen.findByRole("button", { name: "Refresh" }))
    await waitFor(() => expect(calls.some((c) => c.method === "POST" && c.path === "/api/storage/refresh")).toBe(true))
    expect((await screen.findAllByText("measurement started")).length).toBeGreaterThan(0)
  })

  it("shows a running measurement and a failed one", async () => {
    mockApi(routes({ "GET /api/storage": { ...fixtures.storage, measuring: true, measure_error: "du failed" } }))
    renderApp("/storage")
    expect(await screen.findByText("measuring…")).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Refresh" })).toBeDisabled()
    expect(screen.getByText("✖ du failed")).toBeInTheDocument()
  })
})

describe("Recent operations card", () => {
  it("lists finished operations", async () => {
    mockApi(routes())
    renderApp("/storage")
    const row = within((await screen.findByText("cleared NuGet (3.6 GB freed)")).closest("tr") as HTMLElement)
    expect(row.getByText("13:06")).toBeInTheDocument()
    expect(row.getByText("clear")).toBeInTheDocument()
    expect(row.getByText("nuget")).toBeInTheDocument()
    expect(row.getByText("ok")).toBeInTheDocument()
  })

  it("says when nothing has run", async () => {
    mockApi(routes({ "GET /api/storage": { ...fixtures.storage, operations: { current: null, queued: 0, recent: null } } }))
    renderApp("/storage")
    expect(await screen.findByText("no operations since the daemon started")).toBeInTheDocument()
  })

  it(
    "toasts operations that finish while the page is open, failures first",
    async () => {
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
      expect(screen.queryByText("clear nuget: cleared NuGet (3.6 GB freed)")).toBeNull()
      const toasts = await screen.findAllByText(
        "remove go 1.23.1: permission denied · install node 24: installed node 24.9.0",
        undefined,
        { timeout: 4000 },
      )
      expect(toasts.length).toBeGreaterThan(0)
    },
    { timeout: 10_000 },
  )
})
```

- [ ] **Step 2: Run it to see it fail**

Run: `timeout 300 npm --prefix web test -- src/components/storage-cards.test.tsx`
Expected: FAIL, `41000 files` is not found.

- [ ] **Step 3: Implement the two cards and the operation toasts**

Create `web/src/components/storage-cards.tsx`:

```tsx
import { useMutation, useQueryClient } from "@tanstack/react-query"
import { Badge, type BadgeVariant } from "darkraise-ui/components/badge"
import { Button } from "darkraise-ui/components/button"
import { Card, CardContent, CardHeader, CardTitle } from "darkraise-ui/components/card"
import { toast } from "darkraise-ui/components/sonner"
import { Table, TableBody, TableCell, TableRow } from "darkraise-ui/components/table"
import { useEffect, useRef, useState } from "react"
import { api } from "@/api/client"
import { keys } from "@/api/hooks"
import type { PackageCache, Status, Storage } from "@/api/types"
import { ConfirmDialog, type Confirm } from "@/components/confirm-dialog"
import { RefusedHint } from "@/components/refused-hint"
import { ago, hhmm, humanBytes } from "@/lib/format"
import { useNow } from "@/lib/use-now"

const OUTCOME: Record<string, BadgeVariant> = { ok: "green", refused: "amber", interrupted: "amber", failed: "red" }

export function PackageCachesCard({ storage, status, offline }: { storage: Storage; status: Status | undefined; offline: boolean }) {
  const queryClient = useQueryClient()
  const now = useNow()
  const [confirm, setConfirm] = useState<Confirm | null>(null)
  const settled = () => queryClient.invalidateQueries({ queryKey: keys.storage })
  const clear = useMutation({
    mutationFn: (c: PackageCache) => api.clearCache(c.name),
    onSuccess: (_data, c) => {
      toast.success(`queued: clear ${c.label}`)
    },
    onSettled: settled,
  })
  const refresh = useMutation({
    mutationFn: () => api.refreshStorage(),
    onSuccess: () => {
      toast.success("measurement started")
    },
    onSettled: settled,
  })
  const measured = storage.measuring ? (
    <span className="text-amber-600">measuring…</span>
  ) : storage.measured_at ? (
    `measured ${hhmm(new Date(storage.measured_at))}`
  ) : (
    "not measured yet"
  )

  return (
    <Card>
      <CardHeader>
        <CardTitle>Package caches</CardTitle>
      </CardHeader>
      <CardContent className="flex flex-col gap-3">
        <Table>
          <TableBody>
            {(storage.package_caches ?? []).map((c) =>
              c.present ? (
                <TableRow key={c.name}>
                  <TableCell>{c.label}</TableCell>
                  <TableCell className="max-w-48 truncate text-muted-foreground">{(c.paths ?? []).join(" ")}</TableCell>
                  <TableCell>{humanBytes(c.bytes)}</TableCell>
                  <TableCell>{`${c.files} files`}</TableCell>
                  <TableCell className="text-muted-foreground">
                    {c.last_written ? `written ${ago(now - Date.parse(c.last_written))}` : "never written"}
                  </TableCell>
                  <TableCell className="text-right">
                    <Button
                      size="sm"
                      variant="destructive"
                      aria-label={`Clear ${c.label}`}
                      disabled={offline}
                      onClick={() =>
                        setConfirm({
                          title: `Clear the ${c.label} cache (${humanBytes(c.bytes)})? Jobs download what they need again.`,
                          action: "Clear",
                          destructive: true,
                          run: () => clear.mutate(c),
                        })
                      }
                    >
                      Clear
                    </Button>
                  </TableCell>
                </TableRow>
              ) : (
                <TableRow key={c.name}>
                  <TableCell>{c.label}</TableCell>
                  <TableCell colSpan={5} className="text-muted-foreground">
                    not present
                  </TableCell>
                </TableRow>
              ),
            )}
          </TableBody>
        </Table>
        <RefusedHint what="Clear" status={status} />
        <div className="flex items-center justify-end gap-2 text-sm text-muted-foreground">
          <span>{measured}</span>
          <Button size="sm" variant="outline" disabled={offline || storage.measuring} onClick={() => refresh.mutate()}>
            Refresh
          </Button>
        </div>
        {storage.measure_error && <p className="text-sm text-destructive">✖ {storage.measure_error}</p>}
      </CardContent>
      <ConfirmDialog confirm={confirm} onClose={() => setConfirm(null)} />
    </Card>
  )
}

export function OperationsCard({ storage }: { storage: Storage }) {
  const recent = storage.operations.recent ?? []
  return (
    <Card>
      <CardHeader>
        <CardTitle>Recent operations</CardTitle>
      </CardHeader>
      <CardContent>
        {recent.length === 0 ? (
          <p className="text-sm text-muted-foreground">no operations since the daemon started</p>
        ) : (
          <Table>
            <TableBody>
              {recent.map((o) => (
                <TableRow key={o.id}>
                  <TableCell>{hhmm(new Date(o.finished_at ?? o.started_at))}</TableCell>
                  <TableCell>{o.kind}</TableCell>
                  <TableCell>{o.target}</TableCell>
                  <TableCell>
                    <Badge variant={OUTCOME[o.outcome ?? ""] ?? "secondary"} size="sm">
                      {o.outcome}
                    </Badge>
                  </TableCell>
                  <TableCell className="text-muted-foreground">{o.message}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </CardContent>
    </Card>
  )
}

// One toast per snapshot names every operation that finished since the last
// one, failures first. The first snapshot only records where the page starts,
// so opening it does not replay old results.
export function useOperationToasts(storage: Storage | undefined) {
  const last = useRef<string | null | undefined>(undefined)
  useEffect(() => {
    if (!storage) return
    const recent = storage.operations.recent ?? []
    const newest = recent[0]?.id ?? null
    if (last.current === undefined || newest === null || newest === last.current) {
      if (last.current === undefined) last.current = newest
      return
    }
    const seen = recent.findIndex((o) => o.id === last.current)
    const fresh = seen === -1 ? recent : recent.slice(0, seen)
    last.current = newest
    const bad: string[] = []
    const good: string[] = []
    for (const o of fresh) {
      const text = `${o.kind} ${o.target}: ${o.message || o.outcome || ""}`
      if (o.outcome === "ok" || o.outcome === "skipped") good.push(text)
      else bad.push(text)
    }
    const text = [...bad, ...good].join(" · ")
    if (bad.length > 0) toast.error(text)
    else toast.success(text)
  }, [storage])
}
```

- [ ] **Step 4: Put the cards on the page**

In `web/src/pages/storage.tsx`, add the import after the `RefusedHint` import:

```tsx
import { OperationsCard, PackageCachesCard, useOperationToasts } from "@/components/storage-cards"
```

In `StoragePage`, after `const storage = useStorage(status.data)`, add:

```tsx
  useOperationToasts(storage.data)
```

Inside the grid, after the `<ToolchainsCard … />` line, add:

```tsx
          <PackageCachesCard storage={data} status={status.data} offline={offline} />
          <OperationsCard storage={data} />
```

- [ ] **Step 5: Run the checks**

Run: `timeout 300 npm --prefix web test -- src/components/storage-cards.test.tsx src/pages/storage.test.tsx`, then the four web checks.
Expected: all PASS. The times assume vitest's `TZ=UTC`: measured at 14:03, the recent clear finished at 13:06.

- [ ] **Step 6: Commit**

```bash
git add web/src/components/storage-cards.tsx web/src/components/storage-cards.test.tsx web/src/pages/storage.tsx
git commit -m "feat(web): add package caches and recent operations"
```

### Task 15: web: Settings form

**Files:**
- Modify: `web/src/pages/settings.tsx` (replace), `web/src/router.test.tsx` (delete one test)
- Delete: `web/src/pages/not-yet.tsx`
- Test: `web/src/pages/settings.test.tsx`

**Interfaces:**
- Consumes: C3 (`api.patchConfig`, `api.config`), C5 (`keys`, `useConfig`, `useStatus`), C6 (`TagField`), C7 (`DAY_MS`, `durationError`, `sameDuration`), C8 (`changedKeys`, `sameValue`, `settle`, `SaveBar`, `RejectedAlert`, `UnsavedGuard`).
- Produces: C10 `SettingsPage`. The page ends with a `SaveBar` element, and Tasks 16 and 17 insert their cards directly before it.

**Items:** 13
**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 1 = 4

The TUI's Settings fields, in order, with their labels and descriptions verbatim (`internal/tui/settings.go:117-139, 420-441`):

| Section | Label | Description | Key | Input |
|---|---|---|---|---|
| General | `Mode` | the selected option's text | `mode` | select: `queue` = `start runners only for queued jobs, up to the global max`, `all` = `keep warm runners per repo, up to each repo's max` |
| General | `Global max` | `applies in queue mode` | `global_max` | number 1-99 |
| General | `Owner` | `change in config.yaml and restart the daemon` | `owner` | read-only, never sent |
| Timing | `Poll interval` | `how often GitHub is checked (at least 5s)` | `poll_interval` | duration, floor 5s |
| Timing | `Start timeout` | `a runner not online by then is replaced` | `start_timeout` | duration, floor 0 |
| Timing | `Idle timeout` | `idle runners beyond warm stop after this` | `idle_timeout` | duration, floor 0 |
| Disk and retention | `Disk high-water` | `disk use that triggers pruning` | `disk_high_water` | number 1-100, step 5, suffix `%` |
| Disk and retention | `Build cache keep` | `build cache kept when pruning, e.g. 20GB` | `build_cache_keep` | text |
| Disk and retention | `History retention` | `history and logs older than this are removed (at least 1d)` | `history_retention` | duration, floor 1d |
| Runner defaults (note `limits apply to newly started runners`) | `Global labels` | `added to every runner` | `labels` | `TagField` |
| Runner defaults | `Memory max` | `per runner, e.g. 6G, 50% or infinity` | `runner_limits.memory_max` | text |
| Runner defaults | `CPU quota` | `per runner, e.g. 200%` | `runner_limits.cpu_quota` | text |

The number checks reuse the daemon's own messages (`internal/config/config.go:279-370`): `global_max must be >= 1` and `disk_high_water must be 1..100`.

- [ ] **Step 1: Write the failing test**

Create `web/src/pages/settings.test.tsx`:

```tsx
import { screen, waitFor } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import type { Config } from "@/api/types"
import { json, mockApi, noContent } from "@/test/api"
import { authedRoutes, fixtures } from "@/test/fixtures"
import { renderApp } from "@/test/render"

function routes(over: Record<string, unknown> = {}) {
  return authedRoutes({ "GET /api/token": fixtures.token, ...over })
}

// A daemon that applies each PATCH /config to the config it then serves.
function applyingDaemon() {
  let config: Config = fixtures.config
  return routes({
    "GET /api/config": () => config,
    "PATCH /api/config": ({ body }: { body: unknown }) => {
      const { runner_limits, ...rest } = body as Partial<Config>
      config = { ...config, ...rest, runner_limits: { ...config.runner_limits, ...runner_limits } }
      return noContent()
    },
  })
}

describe("Settings page", () => {
  it("shows the loaded settings", async () => {
    mockApi(routes())
    renderApp("/settings")
    expect(await screen.findByLabelText("Poll interval")).toHaveValue("10s")
    expect(screen.getByLabelText("Start timeout")).toHaveValue("2m0s")
    expect(screen.getByLabelText("Global max")).toHaveValue(2)
    expect(screen.getByLabelText("Disk high-water")).toHaveValue(80)
    expect(screen.getByLabelText("Memory max")).toHaveValue("6G")
    expect(screen.getByText("darkraise")).toBeInTheDocument()
    expect(screen.getByText("homelab")).toBeInTheDocument()
    expect(screen.getByText("start runners only for queued jobs, up to the global max")).toBeInTheDocument()
    expect(screen.getByText("limits apply to newly started runners")).toBeInTheDocument()
    expect(screen.queryByText(/unsaved change/)).toBeNull()
  })

  it("saves only the changed fields", async () => {
    const { calls } = mockApi(applyingDaemon())
    const { user } = renderApp("/settings")
    const poll = await screen.findByLabelText("Poll interval")
    await user.clear(poll)
    await user.type(poll, "15s")
    await user.clear(screen.getByLabelText("Global max"))
    await user.type(screen.getByLabelText("Global max"), "3")
    await user.clear(screen.getByLabelText("Memory max"))
    await user.type(screen.getByLabelText("Memory max"), "8G")
    await user.type(screen.getByRole("textbox", { name: "Global labels" }), "gpu{Enter}")
    expect(screen.getByText("● 4 unsaved changes")).toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: "Save changes" }))
    await waitFor(() => expect(calls.some((c) => c.method === "PATCH")).toBe(true))
    expect(calls.find((c) => c.method === "PATCH")?.body).toEqual({
      global_max: 3,
      poll_interval: "15s",
      labels: ["homelab", "gpu"],
      runner_limits: { memory_max: "8G" },
    })
    expect((await screen.findAllByText("Settings saved")).length).toBeGreaterThan(0)
    await waitFor(() => expect(screen.queryByText(/unsaved change/)).toBeNull())
    expect(screen.getByLabelText("Poll interval")).toHaveValue("15s")
    expect(screen.queryByText(/daemon did not apply/)).toBeNull()
  })

  it("disables the fields while the daemon is unreachable", async () => {
    mockApi(routes({ "GET /api/status": () => json({ error: "connection refused" }, 502) }))
    renderApp("/settings")
    await waitFor(() => expect(screen.getByLabelText("Poll interval")).toBeDisabled())
    expect(screen.getByLabelText("Global max")).toBeDisabled()
  })

  it("switches the mode", async () => {
    const { calls } = mockApi(applyingDaemon())
    const { user } = renderApp("/settings")
    await screen.findByLabelText("Poll interval")
    await user.click(screen.getByRole("combobox", { name: "Mode" }))
    await user.click(await screen.findByRole("option", { name: "all" }))
    expect(screen.getByText("keep warm runners per repo, up to each repo's max")).toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: "Save changes" }))
    await waitFor(() => expect(calls.find((c) => c.method === "PATCH")?.body).toEqual({ mode: "all" }))
  })

  it("does not count an equal duration as a change", async () => {
    mockApi(routes())
    const { user } = renderApp("/settings")
    const start = await screen.findByLabelText("Start timeout")
    await user.clear(start)
    await user.type(start, "120s")
    expect(screen.queryByText(/unsaved change/)).toBeNull()
  })

  it("checks the fields before sending", async () => {
    const { calls } = mockApi(routes())
    const { user } = renderApp("/settings")
    const poll = await screen.findByLabelText("Poll interval")
    await user.clear(poll)
    await user.type(poll, "2s")
    expect(screen.getByText("✖ poll_interval must be at least 5s")).toBeInTheDocument()
    await user.clear(screen.getByLabelText("Global max"))
    expect(screen.getByText("✖ global_max must be >= 1")).toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: "Save changes" }))
    expect((await screen.findAllByText("fix the highlighted settings first")).length).toBeGreaterThan(0)
    expect(calls.some((c) => c.method === "PATCH")).toBe(false)
  })

  it("lists the daemon's reasons when it rejects a save, and keeps the edits", async () => {
    mockApi(
      routes({
        "PATCH /api/config": () =>
          json(
            { error: "build_cache_keep must look like 20GB; runner_limits.cpu_quota must be a positive percentage such as 200%" },
            400,
          ),
      }),
    )
    const { user } = renderApp("/settings")
    const keep = await screen.findByLabelText("Build cache keep")
    await user.clear(keep)
    await user.type(keep, "lots")
    await user.click(screen.getByRole("button", { name: "Save changes" }))
    expect(await screen.findByText("Save rejected")).toBeInTheDocument()
    expect(screen.getByText("✖ build_cache_keep must look like 20GB")).toBeInTheDocument()
    expect(screen.getByText("✖ runner_limits.cpu_quota must be a positive percentage such as 200%")).toBeInTheDocument()
    expect((await screen.findAllByText("settings not saved")).length).toBeGreaterThan(0)
    expect(screen.getByLabelText("Build cache keep")).toHaveValue("lots")
  })

  it("names a failed save that is not a rejection", async () => {
    mockApi(routes({ "PATCH /api/config": () => json({ error: "ghr is shutting down" }, 503) }))
    const { user } = renderApp("/settings")
    const max = await screen.findByLabelText("Global max")
    await user.clear(max)
    await user.type(max, "3")
    await user.click(screen.getByRole("button", { name: "Save changes" }))
    expect((await screen.findAllByText("settings not saved: ghr is shutting down")).length).toBeGreaterThan(0)
    expect(screen.queryByText("Save rejected")).toBeNull()
  })

  it("says when the daemon did not apply a saved field", async () => {
    mockApi(routes({ "PATCH /api/config": () => noContent() }))
    const { user } = renderApp("/settings")
    const max = await screen.findByLabelText("Global max")
    await user.clear(max)
    await user.type(max, "3")
    await user.click(screen.getByRole("button", { name: "Save changes" }))
    expect((await screen.findAllByText("daemon did not apply global_max; is it older than this ghr?")).length).toBeGreaterThan(0)
  })

  it("discards the edits", async () => {
    mockApi(routes())
    const { user } = renderApp("/settings")
    const poll = await screen.findByLabelText("Poll interval")
    await user.clear(poll)
    await user.type(poll, "15s")
    await user.click(screen.getByRole("button", { name: "Discard" }))
    expect(screen.getByLabelText("Poll interval")).toHaveValue("10s")
    expect(screen.queryByText(/unsaved change/)).toBeNull()
  })

  it("asks before leaving with unsaved changes", async () => {
    mockApi(routes())
    const { user, router } = renderApp("/settings")
    const poll = await screen.findByLabelText("Poll interval")
    await user.clear(poll)
    await user.type(poll, "15s")
    await user.click(screen.getAllByRole("link", { name: "Dashboard" })[0] as HTMLElement)
    expect(await screen.findByText("You have 1 unsaved change on the Settings page.")).toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: "Stay" }))
    expect(router.state.location.pathname).toBe("/settings")
  })
})
```

- [ ] **Step 2: Run it to see it fail**

Run: `timeout 300 npm --prefix web test -- src/pages/settings.test.tsx`
Expected: FAIL; the placeholder page has no `Poll interval` field.

- [ ] **Step 3: Implement the page**

Replace `web/src/pages/settings.tsx` with:

```tsx
import { useQueryClient } from "@tanstack/react-query"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "darkraise-ui/components/card"
import { Input } from "darkraise-ui/components/input"
import { Label } from "darkraise-ui/components/label"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "darkraise-ui/components/select"
import { toast } from "darkraise-ui/components/sonner"
import { PageHeader } from "darkraise-ui/layout"
import { useState, type ReactNode } from "react"
import { ApiError, api } from "@/api/client"
import { keys, useConfig, useStatus } from "@/api/hooks"
import type { Config, ConfigPatch, RunnerLimitsPatch } from "@/api/types"
import { RejectedAlert, SaveBar } from "@/components/save-bar"
import { TagField } from "@/components/tag-field"
import { UnsavedGuard } from "@/components/unsaved-guard"
import { changedKeys, sameValue, settle, type Equal, type Values } from "@/lib/draft"
import { DAY_MS, durationError, sameDuration } from "@/lib/duration"
import { errorText } from "@/query"

const MODES: Record<string, string> = {
  queue: "start runners only for queued jobs, up to the global max",
  all: "keep warm runners per repo, up to each repo's max",
}

const FLOORS: Record<string, number> = { poll_interval: 5000, start_timeout: 0, idle_timeout: 0, history_retention: DAY_MS }

function loadedValues(c: Config): Values {
  return {
    mode: c.mode,
    global_max: c.global_max,
    poll_interval: c.poll_interval,
    start_timeout: c.start_timeout,
    idle_timeout: c.idle_timeout,
    disk_high_water: c.disk_high_water,
    build_cache_keep: c.build_cache_keep,
    history_retention: c.history_retention,
    labels: c.labels ?? [],
    memory_max: c.runner_limits.memory_max,
    cpu_quota: c.runner_limits.cpu_quota,
  }
}

const equal: Equal = (key, a, b) => (key in FLOORS ? sameDuration(String(a), String(b)) : sameValue(a, b))

function fieldErrors(v: Values): Record<string, string> {
  const errors: Record<string, string> = {}
  for (const [key, floor] of Object.entries(FLOORS)) {
    const err = durationError(key, String(v[key] ?? ""), floor)
    if (err) errors[key] = err
  }
  const max = Number(v.global_max)
  if (!Number.isInteger(max) || max < 1) errors.global_max = "global_max must be >= 1"
  const high = Number(v.disk_high_water)
  if (!Number.isInteger(high) || high < 1 || high > 100) errors.disk_high_water = "disk_high_water must be 1..100"
  return errors
}

const isLimit = (key: string): key is keyof RunnerLimitsPatch => key === "memory_max" || key === "cpu_quota"

function toPatch(sent: Values): ConfigPatch {
  const patch: Record<string, unknown> = {}
  const limits: RunnerLimitsPatch = {}
  for (const [key, v] of Object.entries(sent)) {
    const value = typeof v === "string" ? v.trim() : v
    if (isLimit(key)) limits[key] = value as string
    else patch[key] = value
  }
  if (Object.keys(limits).length > 0) patch.runner_limits = limits
  return patch as ConfigPatch
}

const patchKey = (key: string) => (isLimit(key) ? `runner_limits.${key}` : key)

function Section({ title, note, children }: { title: string; note?: string; children: ReactNode }) {
  return (
    <Card className="mb-4">
      <CardHeader>
        <CardTitle>{title}</CardTitle>
        {note && <CardDescription>{note}</CardDescription>}
      </CardHeader>
      <CardContent className="flex flex-col gap-4">{children}</CardContent>
    </Card>
  )
}

function Field({
  label,
  htmlFor,
  desc,
  changed,
  error,
  children,
}: {
  label: string
  htmlFor?: string
  desc: string
  changed?: boolean
  error?: string
  children: ReactNode
}) {
  return (
    <div className="grid gap-1 sm:grid-cols-[12rem_1fr] sm:items-start">
      <Label htmlFor={htmlFor} className="sm:pt-2">
        {label}
      </Label>
      <div className="flex flex-col gap-1">
        <div className="flex items-center gap-2">
          {children}
          {changed && (
            <span className="text-amber-600" title="changed">
              ●
            </span>
          )}
        </div>
        {error && <p className="text-sm text-destructive">✖ {error}</p>}
        <p className="text-xs text-muted-foreground">{desc}</p>
      </div>
    </div>
  )
}

export function SettingsPage() {
  const config = useConfig()
  const status = useStatus()
  const queryClient = useQueryClient()
  const [draft, setDraft] = useState<Values>({})
  const [saving, setSaving] = useState(false)
  const [rejection, setRejection] = useState("")

  const loaded = config.data ? loadedValues(config.data) : {}
  const values: Values = { ...loaded, ...draft }
  const changed = config.data ? changedKeys(draft, loaded, equal) : []
  const errors = config.data ? fieldErrors(values) : {}
  const offline = status.isError

  const set = (key: string) => (value: unknown) => setDraft((d) => ({ ...d, [key]: value }))
  const text = (key: string) => String(values[key] ?? "")
  const num = (key: string) => (Number.isNaN(values[key]) ? "" : String(values[key]))
  const toNum = (s: string) => (s === "" ? Number.NaN : Number(s))

  function discard() {
    setDraft({})
    setRejection("")
  }

  async function save(): Promise<boolean> {
    if (changed.length === 0) return true
    if (saving) return false
    if (Object.keys(errors).length > 0) {
      toast.error("fix the highlighted settings first")
      return false
    }
    const sent = Object.fromEntries(changed.map((k) => [k, values[k]]))
    setSaving(true)
    setRejection("")
    try {
      await api.patchConfig(toPatch(sent))
    } catch (err) {
      setSaving(false)
      if (err instanceof ApiError && err.status >= 400 && err.status < 500) {
        setRejection(err.message)
        toast.error("settings not saved")
      } else {
        toast.error(`settings not saved: ${errorText(err)}`)
      }
      return false
    }
    toast.success("Settings saved")
    try {
      const fresh = loadedValues(
        await queryClient.fetchQuery({ queryKey: keys.config, queryFn: ({ signal }) => api.config(signal), staleTime: 0 }),
      )
      for (const key of Object.keys(sent)) {
        if (!equal(key, fresh[key], sent[key])) toast.error(`daemon did not apply ${patchKey(key)}; is it older than this ghr?`)
      }
    } catch (err) {
      toast.error(`saved, but re-reading the config failed: ${errorText(err)}`)
    }
    setDraft((d) => settle(d, sent, equal))
    setSaving(false)
    return true
  }

  const is = (key: string) => changed.includes(key)

  function duration(key: string, label: string, desc: string) {
    return (
      <Field label={label} htmlFor={key} desc={desc} changed={is(key)} error={errors[key]}>
        <Input id={key} className="w-40" value={text(key)} disabled={offline} onChange={(e) => set(key)(e.target.value)} />
      </Field>
    )
  }

  function plain(key: string, label: string, desc: string) {
    return (
      <Field label={label} htmlFor={key} desc={desc} changed={is(key)}>
        <Input id={key} className="w-40" value={text(key)} disabled={offline} onChange={(e) => set(key)(e.target.value)} />
      </Field>
    )
  }

  return (
    <>
      <PageHeader title="Settings" />
      {!config.data ? (
        <p className="text-sm text-muted-foreground">loading…</p>
      ) : (
        <>
          {rejection && <RejectedAlert message={rejection} />}
          <Section title="General">
            <Field label="Mode" desc={MODES[text("mode")] ?? ""} changed={is("mode")}>
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
            <Field label="Global max" htmlFor="global_max" desc="applies in queue mode" changed={is("global_max")} error={errors.global_max}>
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
            <Field label="Owner" desc="change in config.yaml and restart the daemon">
              <span className="font-mono text-sm">{config.data.owner}</span>
            </Field>
          </Section>
          <Section title="Timing">
            {duration("poll_interval", "Poll interval", "how often GitHub is checked (at least 5s)")}
            {duration("start_timeout", "Start timeout", "a runner not online by then is replaced")}
            {duration("idle_timeout", "Idle timeout", "idle runners beyond warm stop after this")}
          </Section>
          <Section title="Disk and retention">
            <Field
              label="Disk high-water"
              htmlFor="disk_high_water"
              desc="disk use that triggers pruning"
              changed={is("disk_high_water")}
              error={errors.disk_high_water}
            >
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
            {plain("build_cache_keep", "Build cache keep", "build cache kept when pruning, e.g. 20GB")}
            {duration("history_retention", "History retention", "history and logs older than this are removed (at least 1d)")}
          </Section>
          <Section title="Runner defaults" note="limits apply to newly started runners">
            <Field label="Global labels" desc="added to every runner" changed={is("labels")}>
              <TagField label="Global labels" value={values.labels as string[]} onChange={set("labels")} disabled={offline} />
            </Field>
            {plain("memory_max", "Memory max", "per runner, e.g. 6G, 50% or infinity")}
            {plain("cpu_quota", "CPU quota", "per runner, e.g. 200%")}
          </Section>
          <SaveBar count={changed.length} saving={saving} disabled={offline} onSave={() => void save()} onDiscard={discard} />
          <UnsavedGuard count={changed.length} page="Settings" saving={saving} onSave={save} onDiscard={discard} />
        </>
      )}
    </>
  )
}
```

- [ ] **Step 4: Remove the last placeholder**

Delete `web/src/pages/not-yet.tsx`. In `web/src/router.test.tsx`, delete the whole `it("shows the later pages as not in the web UI yet", …)` block (Task 11 pointed it at `/settings`). Then confirm nothing else uses it:

Run: `grep -rn "not-yet\|NotYet\|Not in the web UI yet" web/src`
Expected: no output.

- [ ] **Step 5: Run the checks**

Run: `timeout 300 npm --prefix web test -- src/pages/settings.test.tsx src/router.test.tsx`, then the four web checks.
Expected: all PASS.

- [ ] **Step 6: Commit**

```bash
git add web/src/pages/settings.tsx web/src/pages/settings.test.tsx web/src/router.test.tsx web/src/pages/not-yet.tsx
git commit -m "feat(web): add the Settings form"
```

### Task 16: web: GitHub token card

**Files:**
- Create: `web/src/components/token-card.tsx`
- Modify: `web/src/pages/settings.tsx`
- Test: `web/src/components/token-card.test.tsx`

**Interfaces:**
- Consumes: C3 (`api.replaceToken`), C5 (`useToken`, `useStatus`, `keys.token`), C6 (`ConfirmDialog`, `Confirm`), C1 (`StateBadge`, `ago`, `hhmm`, `dateTime`, `useNow`, `errorText`), C7 (`DAY_MS`).
- Produces: `TokenCard()` in `web/src/components/token-card.tsx` (C10).

**Items:** 13, 14
**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 1 = 3

TUI texts (`internal/tui/manage.go:142-295`): card `GitHub token`, note `Replacing checks read access to the first repository only; registration permissions are checked when a runner is next started.`; rows `Status` and `Rate limit`; `expires YYYY-MM-DD (N days)`, `expiry unknown`, `· checked <ago>`, `not read yet`, `expires soon` (state `ok` with fewer than 14 days left); rate `<remaining> / <limit>, resets HH:MM` or `–`; button `Replace token`; dialog `Replace GitHub token` with `Paste a fine-grained token with Administration: read/write and Actions: read on every configured repository.`, buttons `Cancel` and `Replace`, empty input `paste the new token first`; success toast `GitHub token replaced`. The TUI does not confirm a replacement; spec §5 does, so the plan adds the confirmation `Replace the GitHub token? ghr uses the new one at once.` with action `Replace`.

- [ ] **Step 1: Write the failing test**

Create `web/src/components/token-card.test.tsx`:

```tsx
import { screen, waitFor, within } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import type { TokenStatus } from "@/api/types"
import { dateTime } from "@/lib/format"
import { json, mockApi, noContent } from "@/test/api"
import { authedRoutes, fixtures } from "@/test/fixtures"
import { renderApp } from "@/test/render"

const DAY = 86_400_000

function token(over: Partial<TokenStatus> = {}): TokenStatus {
  return {
    ...fixtures.token,
    checked_at: new Date(Date.now() - 60_000).toISOString(),
    expires_at: new Date(Date.now() + 60 * DAY + 3_600_000).toISOString(),
    ...over,
  }
}

describe("GitHub token card", () => {
  it("shows the token's state, expiry and rate limit", async () => {
    const t = token()
    mockApi(authedRoutes({ "GET /api/token": t }))
    renderApp("/settings")
    expect(await screen.findByText(`expires ${dateTime(t.expires_at ?? "").slice(0, 10)} (60 days)`)).toBeInTheDocument()
    expect(screen.getByText("ok")).toBeInTheDocument()
    expect(screen.getByText("· checked 1m ago")).toBeInTheDocument()
    expect(screen.getByText("4980 / 5000, resets 14:45")).toBeInTheDocument()
  })

  it("warns when the token expires within 14 days", async () => {
    mockApi(authedRoutes({ "GET /api/token": token({ expires_at: new Date(Date.now() + 10 * DAY).toISOString() }) }))
    renderApp("/settings")
    expect(await screen.findByText("expires soon")).toBeInTheDocument()
  })

  it("shows a rejected token's reason", async () => {
    mockApi(authedRoutes({ "GET /api/token": { state: "rejected", reason: "Bad credentials" } }))
    renderApp("/settings")
    expect(await screen.findByText("Bad credentials")).toBeInTheDocument()
    expect(screen.getByText("rejected")).toBeInTheDocument()
    expect(screen.getByText("expiry unknown")).toBeInTheDocument()
  })

  it("offers a retry when the token cannot be read", async () => {
    let down = true
    mockApi(authedRoutes({ "GET /api/token": () => (down ? json({ error: "daemon busy" }, 500) : token()) }))
    const { user } = renderApp("/settings")
    expect(await screen.findByText("✖ daemon busy")).toBeInTheDocument()
    down = false
    await user.click(screen.getByRole("button", { name: "Retry" }))
    expect(await screen.findByText("· checked 1m ago")).toBeInTheDocument()
  })

  it("replaces the token only after confirmation, sending it raw", async () => {
    const { calls } = mockApi(authedRoutes({ "GET /api/token": token(), "PUT /api/token": () => noContent() }))
    const { user } = renderApp("/settings")
    await user.click(await screen.findByRole("button", { name: "Replace token" }))
    const dialog = await screen.findByRole("dialog")
    await user.click(within(dialog).getByRole("button", { name: "Replace" }))
    expect(within(dialog).getByText("paste the new token first")).toBeInTheDocument()

    await user.type(within(dialog).getByLabelText("Token"), "github_pat_new")
    await user.click(within(dialog).getByRole("button", { name: "Replace" }))
    const ask = await screen.findByRole("alertdialog")
    expect(within(ask).getByText("Replace the GitHub token? ghr uses the new one at once.")).toBeInTheDocument()
    await user.click(within(ask).getByRole("button", { name: "Cancel" }))
    expect(calls.some((c) => c.method === "PUT")).toBe(false)

    await user.click(await within(dialog).findByRole("button", { name: "Replace" }))
    await user.click(within(await screen.findByRole("alertdialog")).getByRole("button", { name: "Replace" }))
    await waitFor(() => expect(calls.some((c) => c.method === "PUT")).toBe(true))
    expect(calls.find((c) => c.method === "PUT")).toMatchObject({ path: "/api/token", body: "github_pat_new", contentType: "text/plain" })
    expect((await screen.findAllByText("GitHub token replaced")).length).toBeGreaterThan(0)
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull())
  })

  it("shows a rejected token inside the dialog", async () => {
    mockApi(
      authedRoutes({
        "GET /api/token": token(),
        "PUT /api/token": () => json({ error: "new token rejected: Bad credentials" }, 400),
      }),
    )
    const { user } = renderApp("/settings")
    await user.click(await screen.findByRole("button", { name: "Replace token" }))
    const dialog = await screen.findByRole("dialog")
    await user.type(within(dialog).getByLabelText("Token"), "github_pat_bad")
    await user.click(within(dialog).getByRole("button", { name: "Replace" }))
    await user.click(within(await screen.findByRole("alertdialog")).getByRole("button", { name: "Replace" }))
    expect(await screen.findByText("✖ new token rejected: Bad credentials")).toBeInTheDocument()
  })
})
```

- [ ] **Step 2: Run it to see it fail**

Run: `timeout 300 npm --prefix web test -- src/components/token-card.test.tsx`
Expected: FAIL; the Settings page shows no token.

- [ ] **Step 3: Implement the card**

Create `web/src/components/token-card.tsx`:

```tsx
import { useQueryClient } from "@tanstack/react-query"
import { Button } from "darkraise-ui/components/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "darkraise-ui/components/card"
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
import { useState, type ReactNode } from "react"
import { api } from "@/api/client"
import { keys, useStatus, useToken } from "@/api/hooks"
import type { TokenStatus } from "@/api/types"
import { ConfirmDialog, type Confirm } from "@/components/confirm-dialog"
import { StateBadge } from "@/components/state-badge"
import { DAY_MS } from "@/lib/duration"
import { ago, dateTime, hhmm } from "@/lib/format"
import { useNow } from "@/lib/use-now"
import { errorText } from "@/query"

function badge(t: TokenStatus, now: number): string {
  if (t.state === "ok" && t.expires_at && Date.parse(t.expires_at) - now < 14 * DAY_MS) return "expires soon"
  return t.state
}

function expiry(t: TokenStatus, now: number): string {
  if (!t.expires_at) return "expiry unknown"
  return `expires ${dateTime(t.expires_at).slice(0, 10)} (${Math.floor((Date.parse(t.expires_at) - now) / DAY_MS)} days)`
}

function rate(t: TokenStatus | undefined): string {
  if (t?.rate_remaining === undefined) return "–"
  const limit = t.rate_limit !== undefined ? ` / ${t.rate_limit}` : ""
  const reset = t.rate_reset ? `, resets ${hhmm(new Date(t.rate_reset))}` : ""
  return `${t.rate_remaining}${limit}${reset}`
}

function Row({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="flex flex-wrap items-center gap-2 text-sm">
      <span className="w-24 text-muted-foreground">{label}</span>
      {children}
    </div>
  )
}

export function TokenCard() {
  const token = useToken()
  const status = useStatus()
  const queryClient = useQueryClient()
  const now = useNow()
  const [open, setOpen] = useState(false)
  const [text, setText] = useState("")
  const [error, setError] = useState("")
  const [busy, setBusy] = useState(false)
  const [confirm, setConfirm] = useState<Confirm | null>(null)
  const t = token.data

  function close() {
    setOpen(false)
    setText("")
    setError("")
  }

  async function send(value: string) {
    setBusy(true)
    try {
      await api.replaceToken(value)
      close()
      toast.success("GitHub token replaced")
      await queryClient.invalidateQueries({ queryKey: keys.token })
    } catch (err) {
      setError(`✖ ${errorText(err)}`)
    } finally {
      setBusy(false)
    }
  }

  function ask() {
    if (text.trim() === "") return setError("paste the new token first")
    setError("")
    const value = text
    setConfirm({
      title: "Replace the GitHub token? ghr uses the new one at once.",
      action: "Replace",
      destructive: true,
      run: () => void send(value),
    })
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>GitHub token</CardTitle>
        <CardDescription>
          Replacing checks read access to the first repository only; registration permissions are checked when a runner is next started.
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-2">
        {token.isError && (
          <p className="text-sm text-destructive">
            ✖ {errorText(token.error)}{" "}
            <Button variant="link" className="px-1" onClick={() => void token.refetch()}>
              Retry
            </Button>
          </p>
        )}
        <Row label="Status">
          {t ? (
            <>
              <StateBadge state={badge(t, now)} />
              <span>{expiry(t, now)}</span>
              {t.checked_at && <span className="text-muted-foreground">· checked {ago(now - Date.parse(t.checked_at))}</span>}
            </>
          ) : (
            <span className="text-muted-foreground">not read yet</span>
          )}
        </Row>
        <Row label="Rate limit">
          <span>{rate(t)}</span>
        </Row>
        {t?.reason && <p className="text-sm text-destructive">{t.reason}</p>}
        <div>
          <Button disabled={status.isError} onClick={() => setOpen(true)}>
            Replace token
          </Button>
        </div>
      </CardContent>
      <Dialog
        open={open}
        onOpenChange={(o) => {
          if (!o) close()
        }}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Replace GitHub token</DialogTitle>
            <DialogDescription>
              Paste a fine-grained token with Administration: read/write and Actions: read on every configured repository.
            </DialogDescription>
          </DialogHeader>
          <DialogBody className="flex flex-col gap-2">
            <Label htmlFor="new-token">Token</Label>
            <Input id="new-token" type="password" autoComplete="off" value={text} onChange={(e) => setText(e.target.value)} />
            {error && <p className="text-sm text-destructive">{error}</p>}
          </DialogBody>
          <DialogFooter>
            <Button variant="secondary" onClick={close}>
              Cancel
            </Button>
            <Button loading={busy} onClick={ask}>
              Replace
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
      <ConfirmDialog confirm={confirm} onClose={() => setConfirm(null)} />
    </Card>
  )
}
```

Insert the card into the Settings page. In `web/src/pages/settings.tsx`, add `import { TokenCard } from "@/components/token-card"`, and directly before the `<SaveBar` line insert:

```tsx
          <div className="mb-4 grid gap-4 lg:grid-cols-2">
            <TokenCard />
          </div>
```

- [ ] **Step 4: Run the checks**

Run: `timeout 300 npm --prefix web test -- src/components/token-card.test.tsx src/pages/settings.test.tsx`, then the four web checks.
Expected: all PASS. If the confirmation's buttons cannot be clicked while the Replace dialog stays open (user-event reports `pointer-events: none`), or the Replace dialog closes when the confirmation is clicked (it takes the click as outside), close the Replace dialog in `ask` (`setOpen(false)`, keeping `text`). Reopen it from `send`'s `catch` with `setOpen(true)` so the error shows inside it. Find the dialog again in the test after confirming, and record the finding in the commit body.

- [ ] **Step 5: Commit**

```bash
git add web/src/components/token-card.tsx web/src/components/token-card.test.tsx web/src/pages/settings.tsx
git commit -m "feat(web): add the GitHub token card"
```

### Task 17: web: maintenance and account cards

**Files:**
- Create: `web/src/components/maintenance-card.tsx`, `web/src/components/account-card.tsx`
- Modify: `web/src/pages/settings.tsx`
- Test: `web/src/components/settings-cards.test.tsx`

**Interfaces:**
- Consumes: C3 (`api.queueRunnerUpdate`, `api.cancelRunnerUpdate`, `api.reload`, `api.changePassword`, `api.logout`), C5 (`useStatus`, `keys`), C1 (`ago`, `dateTime`, `useNow`, `errorText`), C7 (`DAY_MS`).
- Produces: `MaintenanceCard()` and `AccountCard()` (C10).

**Items:** 13
**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 2 - spec 0 - coupling 1 - risk 1 = 4

TUI texts (`internal/tui/manage.go:302-406`). Card `Maintenance`, row `Runner`; the first matching case wins:
1. no installed version: `version unknown (no dist/current)`;
2. running: `<installed> → <latest>` with badge `updating`;
3. queued: the same with an amber badge `queued` and the line `runs when no job is running or queued`;
4. deadline set: badge `update available` (red when 7 days or fewer remain, or overdue; amber otherwise) and the line `update by YYYY-MM-DD (N days)` or `(overdue)`;
5. checked: `<installed>` with a green badge `up to date` and `checked <ago>`;
6. no check error yet: `<installed> checking…`;
7. otherwise `<installed>`.

A check error adds `last check failed: <err>`. The buttons are `Cancel queued update` (toast `queued runner update cancelled`) and `Queue update` (toast `runner update queued`). `Reload config.yaml` toasts `config reloaded`, `config reloaded (1 warning, see Activity)` or `config reloaded (N warnings, see Activity)`, and a failure toasts `reload rejected: <err>`.

The account card has no TUI counterpart. Its texts are new: card `Account`, fields `Current password`, `New password` and `Confirm new password`, button `Change password`, inline `the current password is wrong`, toast `password changed`, and button `Log out`. The length and match errors reuse the login page's `password must be 12 to 1024 bytes` and `the passwords do not match`.

- [ ] **Step 1: Write the failing test**

Create `web/src/components/settings-cards.test.tsx`:

```tsx
import { screen, waitFor } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import type { RunnerUpdate } from "@/api/types"
import { dateTime } from "@/lib/format"
import { json, mockApi, noContent } from "@/test/api"
import { authedRoutes, fixtures } from "@/test/fixtures"
import { renderApp } from "@/test/render"

const DAY = 86_400_000

function withUpdate(runner_update: RunnerUpdate, over: Record<string, unknown> = {}) {
  return authedRoutes({ "GET /api/token": fixtures.token, "GET /api/status": { ...fixtures.status, runner_update }, ...over })
}

describe("Maintenance card", () => {
  it("cancels a queued runner update", async () => {
    const { calls } = mockApi(withUpdate(fixtures.status.runner_update, { "DELETE /api/runner-update": () => noContent() }))
    const { user } = renderApp("/settings")
    expect(await screen.findByText("runs when no job is running or queued")).toBeInTheDocument()
    expect(screen.getByText("2.337.0 → 2.338.0")).toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: "Cancel queued update" }))
    await waitFor(() => expect(calls.some((c) => c.method === "DELETE" && c.path === "/api/runner-update")).toBe(true))
    expect((await screen.findAllByText("queued runner update cancelled")).length).toBeGreaterThan(0)
  })

  it("queues an available update and shows its deadline", async () => {
    const deadline = new Date(Date.now() + 20 * DAY + 3_600_000).toISOString()
    const { calls } = mockApi(
      withUpdate({ installed: "2.337.0", latest: "2.338.0", deadline }, { "POST /api/runner-update": () => new Response(null, { status: 202 }) }),
    )
    const { user } = renderApp("/settings")
    expect(await screen.findByText("update available")).toBeInTheDocument()
    expect(screen.getByText(`update by ${dateTime(deadline).slice(0, 10)} (20 days)`)).toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: "Queue update" }))
    await waitFor(() => expect(calls.some((c) => c.method === "POST" && c.path === "/api/runner-update")).toBe(true))
    expect((await screen.findAllByText("runner update queued")).length).toBeGreaterThan(0)
  })

  it("says when the runner is up to date", async () => {
    mockApi(withUpdate({ installed: "2.338.0", checked_at: new Date(Date.now() - 2 * 3_600_000).toISOString() }))
    renderApp("/settings")
    expect(await screen.findByText("up to date")).toBeInTheDocument()
    expect(screen.getByText("checked 2h ago")).toBeInTheDocument()
    expect(screen.queryByRole("button", { name: "Queue update" })).toBeNull()
  })

  it("says when the version is unknown", async () => {
    mockApi(withUpdate({}))
    renderApp("/settings")
    expect(await screen.findByText("version unknown (no dist/current)")).toBeInTheDocument()
  })

  it("shows the last failed check", async () => {
    mockApi(withUpdate({ installed: "2.337.0", check_error: "GitHub: 502" }))
    renderApp("/settings")
    expect(await screen.findByText("last check failed: GitHub: 502")).toBeInTheDocument()
  })

  it("reloads the config and counts the warnings", async () => {
    mockApi(withUpdate({}, { "POST /api/reload": ["web settings changed; restart ghr to apply", "another"] }))
    const { user } = renderApp("/settings")
    await user.click(await screen.findByRole("button", { name: "Reload config.yaml" }))
    expect((await screen.findAllByText("config reloaded (2 warnings, see Activity)")).length).toBeGreaterThan(0)
  })

  it("shows a rejected reload", async () => {
    mockApi(withUpdate({}, { "POST /api/reload": () => json({ error: 'mode must be "queue" or "all"' }, 400) }))
    const { user } = renderApp("/settings")
    await user.click(await screen.findByRole("button", { name: "Reload config.yaml" }))
    expect((await screen.findAllByText('reload rejected: mode must be "queue" or "all"')).length).toBeGreaterThan(0)
  })
})

describe("Account card", () => {
  async function fill(user: ReturnType<typeof renderApp>["user"], current: string, next: string, again: string) {
    await user.type(await screen.findByLabelText("Current password"), current)
    await user.type(screen.getByLabelText("New password"), next)
    await user.type(screen.getByLabelText("Confirm new password"), again)
    await user.click(screen.getByRole("button", { name: "Change password" }))
  }

  it("changes the password", async () => {
    const { calls } = mockApi(authedRoutes({ "GET /api/token": fixtures.token, "POST /auth/password": () => noContent() }))
    const { user } = renderApp("/settings")
    await fill(user, "old password 1", "new password 12", "new password 12")
    await waitFor(() => expect(calls.some((c) => c.path === "/auth/password")).toBe(true))
    expect(calls.find((c) => c.path === "/auth/password")?.body).toEqual({ current: "old password 1", new: "new password 12" })
    expect((await screen.findAllByText("password changed")).length).toBeGreaterThan(0)
    expect(screen.getByLabelText("Current password")).toHaveValue("")
  })

  it("checks the new password before sending", async () => {
    const { calls } = mockApi(authedRoutes({ "GET /api/token": fixtures.token }))
    const { user } = renderApp("/settings")
    await fill(user, "old password 1", "short", "short")
    expect(await screen.findByText("password must be 12 to 1024 bytes")).toBeInTheDocument()
    await user.clear(screen.getByLabelText("New password"))
    await user.type(screen.getByLabelText("New password"), "new password 12")
    await user.click(screen.getByRole("button", { name: "Change password" }))
    expect(await screen.findByText("the passwords do not match")).toBeInTheDocument()
    expect(calls.some((c) => c.path === "/auth/password")).toBe(false)
  })

  it("says when the current password is wrong", async () => {
    mockApi(authedRoutes({ "GET /api/token": fixtures.token, "POST /auth/password": () => json({ error: "wrong password" }, 401) }))
    const { user, router } = renderApp("/settings")
    await fill(user, "bad password 1", "new password 12", "new password 12")
    expect(await screen.findByText("the current password is wrong")).toBeInTheDocument()
    expect(router.state.location.pathname).toBe("/settings")
  })

  it("logs out", async () => {
    let authenticated = true
    const { calls } = mockApi(
      authedRoutes({
        "GET /api/token": fixtures.token,
        "GET /auth/state": () => ({ setup_required: false, authenticated }),
        "POST /auth/logout": () => {
          authenticated = false
          return noContent()
        },
      }),
    )
    const { user, router } = renderApp("/settings")
    await user.click(await screen.findByRole("button", { name: "Log out" }))
    await waitFor(() => expect(router.state.location.pathname).toBe("/login"))
    expect(calls.some((c) => c.method === "POST" && c.path === "/auth/logout")).toBe(true)
  })
})
```

- [ ] **Step 2: Run it to see it fail**

Run: `timeout 300 npm --prefix web test -- src/components/settings-cards.test.tsx`
Expected: FAIL; the Settings page has no `Runner` row or password fields.

- [ ] **Step 3: Implement the maintenance card**

Create `web/src/components/maintenance-card.tsx`:

```tsx
import { useMutation, useQueryClient } from "@tanstack/react-query"
import { Badge } from "darkraise-ui/components/badge"
import { Button } from "darkraise-ui/components/button"
import { Card, CardContent, CardHeader, CardTitle } from "darkraise-ui/components/card"
import { toast } from "darkraise-ui/components/sonner"
import { useState, type ReactNode } from "react"
import { api } from "@/api/client"
import { keys, useStatus } from "@/api/hooks"
import type { RunnerUpdate } from "@/api/types"
import { DAY_MS } from "@/lib/duration"
import { ago, dateTime } from "@/lib/format"
import { useNow } from "@/lib/use-now"
import { errorText } from "@/query"

function reloadText(warnings: number): string {
  if (warnings === 0) return "config reloaded"
  return warnings === 1 ? "config reloaded (1 warning, see Activity)" : `config reloaded (${warnings} warnings, see Activity)`
}

function RunnerVersion({ u, now }: { u: RunnerUpdate; now: number }) {
  const versions = `${u.installed} → ${u.latest}`
  let main: ReactNode = u.installed
  let note = ""
  if (!u.installed) {
    main = <span className="text-muted-foreground">version unknown (no dist/current)</span>
  } else if (u.running) {
    main = (
      <>
        <span>{versions}</span>
        <Badge variant="blue" size="sm">
          updating
        </Badge>
      </>
    )
  } else if (u.queued) {
    main = (
      <>
        <span>{versions}</span>
        <Badge variant="amber" size="sm">
          queued
        </Badge>
      </>
    )
    note = "runs when no job is running or queued"
  } else if (u.deadline) {
    const left = Date.parse(u.deadline) - now
    main = (
      <>
        <span>{versions}</span>
        <Badge variant={left <= 7 * DAY_MS ? "red" : "amber"} size="sm">
          update available
        </Badge>
      </>
    )
    note = `update by ${dateTime(u.deadline).slice(0, 10)} (${left <= 0 ? "overdue" : `${Math.floor(left / DAY_MS)} days`})`
  } else if (u.checked_at) {
    main = (
      <>
        <span>{u.installed}</span>
        <Badge variant="green" size="sm">
          up to date
        </Badge>
        <span className="text-muted-foreground">checked {ago(now - Date.parse(u.checked_at))}</span>
      </>
    )
  } else if (!u.check_error) {
    main = <span>{u.installed} checking…</span>
  }
  return (
    <div className="flex flex-col gap-1 text-sm">
      <div className="flex flex-wrap items-center gap-2">
        <span className="w-24 text-muted-foreground">Runner</span>
        {main}
      </div>
      {note && <p className="text-muted-foreground">{note}</p>}
      {u.check_error && <p className="text-muted-foreground">last check failed: {u.check_error}</p>}
    </div>
  )
}

export function MaintenanceCard() {
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
      toast.success(cancel ? "queued runner update cancelled" : "runner update queued")
    },
    onSettled: () => refresh(),
  })

  async function reload() {
    setReloading(true)
    try {
      toast.success(reloadText((await api.reload()).length))
    } catch (err) {
      toast.error(`reload rejected: ${errorText(err)}`)
    } finally {
      setReloading(false)
      await refresh()
    }
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>Maintenance</CardTitle>
      </CardHeader>
      <CardContent className="flex flex-col gap-3">
        {u ? <RunnerVersion u={u} now={now} /> : <p className="text-sm text-muted-foreground">loading…</p>}
        <div className="flex flex-wrap gap-2">
          {u?.queued && (
            <Button variant="secondary" disabled={offline || update.isPending} onClick={() => update.mutate(true)}>
              Cancel queued update
            </Button>
          )}
          {u?.deadline && !u.running && !u.queued && (
            <Button disabled={offline || update.isPending} onClick={() => update.mutate(false)}>
              Queue update
            </Button>
          )}
          <Button disabled={offline} loading={reloading} onClick={() => void reload()}>
            Reload config.yaml
          </Button>
        </div>
      </CardContent>
    </Card>
  )
}
```

- [ ] **Step 4: Implement the account card**

Create `web/src/components/account-card.tsx`:

```tsx
import { useQueryClient } from "@tanstack/react-query"
import { useNavigate } from "@tanstack/react-router"
import { Button } from "darkraise-ui/components/button"
import { Card, CardContent, CardHeader, CardTitle } from "darkraise-ui/components/card"
import { Input } from "darkraise-ui/components/input"
import { Label } from "darkraise-ui/components/label"
import { toast } from "darkraise-ui/components/sonner"
import { useState, type FormEvent } from "react"
import { ApiError, api } from "@/api/client"
import { errorText } from "@/query"

function lengthOk(password: string): boolean {
  const n = new TextEncoder().encode(password).length
  return n >= 12 && n <= 1024
}

export function AccountCard() {
  const queryClient = useQueryClient()
  const navigate = useNavigate()
  const [current, setCurrent] = useState("")
  const [next, setNext] = useState("")
  const [again, setAgain] = useState("")
  const [error, setError] = useState("")
  const [busy, setBusy] = useState(false)

  async function change(e: FormEvent) {
    e.preventDefault()
    setError("")
    if (!lengthOk(next)) return setError("password must be 12 to 1024 bytes")
    if (next !== again) return setError("the passwords do not match")
    setBusy(true)
    try {
      await api.changePassword(current, next)
      toast.success("password changed")
      setCurrent("")
      setNext("")
      setAgain("")
    } catch (err) {
      setError(err instanceof ApiError && err.status === 401 ? "the current password is wrong" : errorText(err))
    } finally {
      setBusy(false)
    }
  }

  async function logout() {
    try {
      await api.logout()
    } catch {
      // a session that already ended still lands on the login page
    }
    queryClient.clear()
    await navigate({ to: "/login" })
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>Account</CardTitle>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        <form className="flex flex-col gap-3" onSubmit={(e) => void change(e)}>
          <div className="flex flex-col gap-1">
            <Label htmlFor="current-password">Current password</Label>
            <Input
              id="current-password"
              type="password"
              autoComplete="current-password"
              value={current}
              onChange={(e) => setCurrent(e.target.value)}
            />
          </div>
          <div className="flex flex-col gap-1">
            <Label htmlFor="new-password">New password</Label>
            <Input id="new-password" type="password" autoComplete="new-password" value={next} onChange={(e) => setNext(e.target.value)} />
          </div>
          <div className="flex flex-col gap-1">
            <Label htmlFor="confirm-password">Confirm new password</Label>
            <Input
              id="confirm-password"
              type="password"
              autoComplete="new-password"
              value={again}
              onChange={(e) => setAgain(e.target.value)}
            />
          </div>
          {error && <p className="text-sm text-destructive">{error}</p>}
          <div>
            <Button type="submit" loading={busy}>
              Change password
            </Button>
          </div>
        </form>
        <div>
          <Button variant="outline" onClick={() => void logout()}>
            Log out
          </Button>
        </div>
      </CardContent>
    </Card>
  )
}
```

- [ ] **Step 5: Wire both cards into the Settings page**

In `web/src/pages/settings.tsx`, add `import { AccountCard } from "@/components/account-card"` and `import { MaintenanceCard } from "@/components/maintenance-card"`. Then replace the grid Task 16 inserted:

```tsx
          <div className="mb-4 grid gap-4 lg:grid-cols-2">
            <TokenCard />
          </div>
```

with:

```tsx
          <div className="mb-4 grid gap-4 lg:grid-cols-2">
            <TokenCard />
            <MaintenanceCard />
            <AccountCard />
          </div>
```

- [ ] **Step 6: Run the checks**

Run: `timeout 300 npm --prefix web test -- src/components/settings-cards.test.tsx src/components/token-card.test.tsx src/pages/settings.test.tsx`, then the four web checks.
Expected: all PASS.

- [ ] **Step 7: Commit**

```bash
git add web/src/components/maintenance-card.tsx web/src/components/account-card.tsx web/src/components/settings-cards.test.tsx web/src/pages/settings.tsx
git commit -m "feat(web): add maintenance and account settings"
```

### Task 18: web: login redirect and 401 guard

**Files:**
- Modify: `web/src/router.tsx`, `web/src/app.tsx`, `web/src/pages/login.tsx`
- Test: `web/src/pages/login.test.tsx`, `web/src/app.test.tsx`

**Interfaces:**
- Consumes: C1 (`renderApp`, `authedRoutes`, `fixtures`, `api`), C4 (`mockApi`), C9 (the route tree as Task 8 left it).
- Produces: C9's `/login` search `{ redirect?: string }`. Every key is optional, so existing `navigate({ to: "/login" })` calls (Shell's log out, Task 17's account card) need no `search`.

**Items:** 17, 18
**Implementer:** dr-superpowers:impl-sonnet-high
**Evaluation:** files 2 - spec 0 - coupling 1 - risk 2 = 5

- [ ] **Step 1: Write the failing tests**

In `web/src/pages/login.test.tsx`, change the import line `import { authedRoutes } from "@/test/fixtures"` to `import { authedRoutes, fixtures } from "@/test/fixtures"`. In the test `"shows a wrong password inline"`, add this line after `expect(await screen.findByText("wrong password")).toBeInTheDocument()`:

```ts
    expect(screen.getByRole("alert")).toHaveTextContent("wrong password")
```

Then append these tests inside `describe("login page", …)`:

```ts
  it("returns to the page asked for after logging in", async () => {
    let done = false
    mockApi(
      authedRoutes({
        "GET /auth/state": () => ({ setup_required: false, authenticated: done }),
        "POST /auth/login": () => {
          done = true
          return noContent()
        },
        "GET /api/history": fixtures.history,
      }),
    )
    const { user, router } = renderApp("/history")
    await user.type(await screen.findByLabelText("Password"), "correct horse battery")
    expect(router.state.location.pathname).toBe("/login")
    expect(router.state.location.search).toEqual({ redirect: "/history" })
    await user.click(screen.getByRole("button", { name: "Log in" }))
    await waitFor(() => expect(router.state.location.pathname).toBe("/history"))
  })

  it("ignores a redirect that leaves the site", async () => {
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
    const { user, router } = renderApp("/login?redirect=%2F%2Fevil.example%2Fx")
    await user.type(await screen.findByLabelText("Password"), "correct horse battery")
    await user.click(screen.getByRole("button", { name: "Log in" }))
    await waitFor(() => expect(router.state.location.pathname).toBe("/"))
  })
```

In `web/src/app.test.tsx`, change `import { describe, expect, it } from "vitest"` to `import { describe, expect, it, vi } from "vitest"` and `import { authedRoutes } from "./test/fixtures"` to `import { authedRoutes, fixtures } from "./test/fixtures"`. Then append inside `describe("App", …)`:

```ts
  it("redirects once for a burst of 401s and keeps the page asked for", async () => {
    let expired = false
    const gone = () => json({ error: "not logged in" }, 401)
    mockApi(
      authedRoutes({
        "GET /auth/state": () => ({ setup_required: false, authenticated: !expired }),
        "GET /api/status": () => (expired ? gone() : fixtures.status),
        "GET /api/config": () => (expired ? gone() : fixtures.config),
        "GET /api/history": [],
      }),
    )
    const { router } = renderApp("/history")
    await screen.findByRole("heading", { name: "History" })
    const navigate = vi.spyOn(router, "navigate")
    expired = true
    await act(async () => {
      await Promise.all([api.status().catch(() => undefined), api.config().catch(() => undefined)])
    })
    await waitFor(() => expect(router.state.location.pathname).toBe("/login"))
    expect(router.state.location.search).toEqual({ redirect: "/history" })
    expect(navigate).toHaveBeenCalledTimes(1)
  })
```

- [ ] **Step 2: Run them to see them fail**

Run: `timeout 300 npm --prefix web test -- src/pages/login.test.tsx src/app.test.tsx`
Expected: FAIL. `getByRole("alert")` finds nothing; the redirect test lands on `/` with an empty search ("ignores a redirect that leaves the site" is a guard: it passes before and after the change); the 401 burst calls `navigate` twice.

- [ ] **Step 3: Carry the destination through the login redirect**

In `web/src/router.tsx`, replace `loginRoute` and `appRoute` with:

```tsx
const loginRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/login",
  validateSearch: (search: Record<string, unknown>): { redirect?: string } =>
    typeof search.redirect === "string" ? { redirect: search.redirect } : {},
  beforeLoad: async ({ context }) => {
    if ((await authState(context.queryClient)).authenticated) throw redirect({ to: "/" })
  },
  component: LoginPage,
})

const appRoute = createRoute({
  getParentRoute: () => rootRoute,
  id: "app",
  beforeLoad: async ({ context, location }) => {
    if (!(await authState(context.queryClient)).authenticated) {
      throw redirect({ to: "/login", search: location.href === "/" ? {} : { redirect: location.href } })
    }
  },
  component: Shell,
})
```

In `web/src/app.tsx`, replace the `useEffect` block with:

```tsx
  useEffect(() => {
    // Every poll fails at once when a session expires; one redirect is enough.
    let redirecting = false
    setUnauthorizedHandler(() => {
      const { pathname, href } = router.state.location
      if (redirecting || pathname === "/login") return
      redirecting = true
      queryClient.clear()
      void router
        .navigate({ to: "/login", search: href === "/" ? {} : { redirect: href } })
        .finally(() => {
          redirecting = false
        })
    })
  }, [router, queryClient])
```

In `web/src/pages/login.tsx`:
- change `import { useNavigate } from "@tanstack/react-router"` to `import { useNavigate, useSearch } from "@tanstack/react-router"`;
- add this function after `loginError`:

```tsx
// Only a path on this site: "//host" and "/\host" are read by browsers as
// another origin.
function safeRedirect(target: string | undefined): string {
  return target && target.startsWith("/") && !target.startsWith("//") && !target.startsWith("/\\") ? target : "/"
}
```

- add `const { redirect } = useSearch({ from: "/login" })` as the first line after `const navigate = useNavigate()`;
- in `submit`, replace `await navigate({ to: "/" })` with `await navigate({ href: safeRedirect(redirect) })`;
- replace `{error && <p className="text-sm text-destructive">{error}</p>}` with `{error && <p role="alert" className="text-sm text-destructive">{error}</p>}`.

- [ ] **Step 4: Run the checks**

Run: `timeout 300 npm --prefix web test -- src/pages/login.test.tsx src/app.test.tsx src/router.test.tsx`, then the four web checks.
Expected: all PASS. If `typecheck` reports that a `navigate({ to: "/login" })` call now needs `search`, add `search: {}` to that call. Do not make `redirect` required.

- [ ] **Step 5: Commit**

```bash
git add web/src/router.tsx web/src/app.tsx web/src/pages/login.tsx web/src/pages/login.test.tsx web/src/app.test.tsx
git commit -m "fix(web): return to the asked page after login"
```

### Task 19: web: Stop dialog and row button names

**Files:**
- Modify: `web/src/components/runners-table.tsx`
- Test: `web/src/pages/runners.test.tsx`

**Interfaces:**
- Consumes: C1 (`useStatus`, `keys`, `StateBadge`, `copyText`, `elapsed`, `useNow`, `errorText`).
- Produces: `StopRunnerDialog({ instance, onClose })` keeps its signature. Row buttons are named `Logs for runner <id>`, `Copy ID of runner <id>` and `Stop runner <id>`, and their visible text is unchanged.

**Items:** 14, 17, 18
**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 1 = 3

- [ ] **Step 1: Write the failing tests**

Replace `web/src/pages/runners.test.tsx` with:

```tsx
import { screen, waitFor, within } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"
import { json, mockApi, noContent } from "@/test/api"
import { authedRoutes, fixtures } from "@/test/fixtures"
import { renderApp } from "@/test/render"

async function rowOf(id: string) {
  const cell = await screen.findByRole("link", { name: id })
  const row = cell.closest("tr")
  if (!row) throw new Error(`no row for ${id}`)
  return within(row)
}

describe("Runners page", () => {
  it("lists runners with their state and job", async () => {
    mockApi(authedRoutes())
    renderApp("/runners")
    const busy = await rowOf("aaaaaa")
    expect(busy.getByText("busy")).toBeInTheDocument()
    expect(busy.getByText("test #42")).toBeInTheDocument()
    const idle = await rowOf("bbbbbb")
    expect(idle.getByText("idle")).toBeInTheDocument()
  })

  it("names each row's buttons after its runner", async () => {
    mockApi(authedRoutes())
    renderApp("/runners")
    const row = await rowOf("aaaaaa")
    expect(row.getByRole("button", { name: "Logs for runner aaaaaa" })).toHaveTextContent("Logs")
    expect(row.getByRole("button", { name: "Copy ID of runner aaaaaa" })).toHaveTextContent("Copy ID")
    expect(row.getByRole("button", { name: "Stop runner aaaaaa" })).toHaveTextContent("Stop")
  })

  it("says when no runner is up", async () => {
    mockApi(authedRoutes({ "GET /api/status": { ...fixtures.status, instances: [], repos: [] } }))
    renderApp("/runners")
    expect(await screen.findByText("no runners — they start when jobs are queued")).toBeInTheDocument()
  })

  it("shows repos waiting on their cap", async () => {
    const repos = [{ name: "darkmem", paused: false, max: 1, active: 1, queued: 2 }]
    mockApi(authedRoutes({ "GET /api/status": { ...fixtures.status, instances: [], repos } }))
    renderApp("/runners")
    expect(await screen.findByText("2 jobs queued (repo cap 1)")).toBeInTheDocument()
  })

  it("waits for the daemon while it cannot be reached", async () => {
    mockApi(authedRoutes({ "GET /api/status": () => json({ error: "connection refused" }, 502) }))
    renderApp("/runners")
    expect(await screen.findByText("waiting for the daemon…")).toBeInTheDocument()
  })

  it("stops a runner only after confirmation", async () => {
    const { calls } = mockApi(authedRoutes({ "DELETE /api/runners/aaaaaa": () => noContent() }))
    const { user } = renderApp("/runners")
    const row = await rowOf("aaaaaa")

    await user.click(row.getByRole("button", { name: "Stop runner aaaaaa" }))
    expect(await screen.findByText("Runner aaaaaa is running a job. Stop it?")).toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: "Cancel" }))
    expect(calls.some((c) => c.method === "DELETE")).toBe(false)

    await user.click(row.getByRole("button", { name: "Stop runner aaaaaa" }))
    await user.click(within(await screen.findByRole("alertdialog")).getByRole("button", { name: "Stop" }))
    await waitFor(() => expect(calls.some((c) => c.method === "DELETE" && c.path === "/api/runners/aaaaaa")).toBe(true))
    await waitFor(() => expect(screen.queryByRole("alertdialog")).toBeNull())
    expect((await screen.findAllByText("stopped aaaaaa")).length).toBeGreaterThan(0)
  })

  it("asks plainly before stopping an idle runner", async () => {
    mockApi(authedRoutes())
    const { user } = renderApp("/runners")
    await user.click((await rowOf("bbbbbb")).getByRole("button", { name: "Stop runner bbbbbb" }))
    expect(await screen.findByText("Stop runner bbbbbb?")).toBeInTheDocument()
  })

  it(
    "follows a runner that picks up a job while the dialog is open",
    async () => {
      let busy = false
      mockApi(
        authedRoutes({
          "GET /api/status": () => ({
            ...fixtures.status,
            instances: fixtures.status.instances.map((i) => (busy && i.id === "bbbbbb" ? { ...i, state: "busy" } : i)),
          }),
        }),
      )
      const { user } = renderApp("/runners")
      await user.click((await rowOf("bbbbbb")).getByRole("button", { name: "Stop runner bbbbbb" }))
      expect(await screen.findByText("Stop runner bbbbbb?")).toBeInTheDocument()
      busy = true
      expect(await screen.findByText("Runner bbbbbb is running a job. Stop it?", {}, { timeout: 3000 })).toBeInTheDocument()
    },
    { timeout: 10_000 },
  )

  it("keeps the runner's name in the dialog while it closes", async () => {
    mockApi(authedRoutes())
    const { user } = renderApp("/runners")
    await user.click((await rowOf("bbbbbb")).getByRole("button", { name: "Stop runner bbbbbb" }))
    await screen.findByText("Stop runner bbbbbb?")
    await user.click(screen.getByRole("button", { name: "Cancel" }))
    expect(screen.queryByText(/undefined/)).toBeNull()
  })

  it("copies a runner ID", async () => {
    mockApi(authedRoutes())
    const { user } = renderApp("/runners")
    // userEvent.setup() (inside renderApp) installs its own clipboard stub, so the mock goes in after it.
    Object.defineProperty(window, "isSecureContext", { value: true, configurable: true })
    const writeText = vi.fn(async () => {})
    Object.defineProperty(navigator, "clipboard", { value: { writeText }, configurable: true })
    await user.click((await rowOf("aaaaaa")).getByRole("button", { name: "Copy ID of runner aaaaaa" }))
    expect(writeText).toHaveBeenCalledWith("aaaaaa")
    expect((await screen.findAllByText("copied aaaaaa")).length).toBeGreaterThan(0)
    Reflect.deleteProperty(navigator, "clipboard")
  })

  it("opens the log tab from Logs", async () => {
    mockApi(authedRoutes())
    const { user, router } = renderApp("/runners")
    await user.click((await rowOf("aaaaaa")).getByRole("button", { name: "Logs for runner aaaaaa" }))
    await waitFor(() => expect(router.state.location.pathname).toBe("/runners/aaaaaa"))
    expect(router.state.location.search).toEqual({ tab: "log" })
  })
})
```

- [ ] **Step 2: Run it to see it fail**

Run: `timeout 300 npm --prefix web test -- src/pages/runners.test.tsx`
Expected: FAIL. No button is named `Stop runner aaaaaa`, and the open dialog keeps `Stop runner bbbbbb?` after the runner turns busy. The closing-title test may already pass if jsdom unmounts the dialog at once; it guards the fix.

- [ ] **Step 3: Implement**

Replace `web/src/components/runners-table.tsx` with:

```tsx
import { useMutation, useQueryClient } from "@tanstack/react-query"
import { Link, useNavigate } from "@tanstack/react-router"
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
import { Badge } from "darkraise-ui/components/badge"
import { Button } from "darkraise-ui/components/button"
import { toast } from "darkraise-ui/components/sonner"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "darkraise-ui/components/table"
import { useState } from "react"
import { api } from "@/api/client"
import { keys, useStatus } from "@/api/hooks"
import type { InstanceStatus, Status } from "@/api/types"
import { StateBadge } from "@/components/state-badge"
import { copyText } from "@/lib/clipboard"
import { elapsed } from "@/lib/format"
import { useNow } from "@/lib/use-now"
import { errorText } from "@/query"

export function RunnersTable({ status, actions }: { status: Status; actions: boolean }) {
  const navigate = useNavigate()
  const now = useNow()
  const [stopping, setStopping] = useState<InstanceStatus | null>(null)
  const waiting = status.repos.filter((r) => !r.paused && r.queued > 0 && r.max > 0 && r.active >= r.max)

  if (status.instances.length === 0 && waiting.length === 0) {
    return <p className="py-6 text-center text-sm text-muted-foreground">no runners — they start when jobs are queued</p>
  }

  async function copy(id: string) {
    try {
      await copyText(id)
      toast.success(`copied ${id}`)
    } catch (err) {
      toast.error(errorText(err))
    }
  }

  return (
    <>
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>ID</TableHead>
            <TableHead>Repo</TableHead>
            <TableHead>State</TableHead>
            <TableHead>Job</TableHead>
            <TableHead>Elapsed</TableHead>
            {actions && <TableHead className="text-right">Actions</TableHead>}
          </TableRow>
        </TableHeader>
        <TableBody>
          {status.instances.map((i) => (
            <TableRow key={i.id}>
              <TableCell className="font-mono">
                <Link to="/runners/$id" params={{ id: i.id }} search={{ tab: "steps" }} className="hover:underline">
                  {i.id}
                </Link>
              </TableCell>
              <TableCell>{i.repo}</TableCell>
              <TableCell>
                <StateBadge state={i.state} />
              </TableCell>
              <TableCell>{i.job ? `${i.job.name} #${i.job.run_number}` : "–"}</TableCell>
              <TableCell>{elapsed(i, now)}</TableCell>
              {actions && (
                <TableCell>
                  <div className="flex justify-end gap-2">
                    <Button
                      size="sm"
                      variant="outline"
                      aria-label={`Logs for runner ${i.id}`}
                      onClick={() => void navigate({ to: "/runners/$id", params: { id: i.id }, search: { tab: "log" } })}
                    >
                      Logs
                    </Button>
                    <Button size="sm" variant="outline" aria-label={`Copy ID of runner ${i.id}`} onClick={() => void copy(i.id)}>
                      Copy ID
                    </Button>
                    <Button size="sm" variant="destructive" aria-label={`Stop runner ${i.id}`} onClick={() => setStopping(i)}>
                      Stop
                    </Button>
                  </div>
                </TableCell>
              )}
            </TableRow>
          ))}
          {waiting.map((r) => (
            <TableRow key={`waiting-${r.name}`}>
              <TableCell>–</TableCell>
              <TableCell>{r.name}</TableCell>
              <TableCell>
                <Badge variant="amber" size="sm">
                  ⧗ waiting
                </Badge>
              </TableCell>
              <TableCell colSpan={actions ? 3 : 2}>
                {r.queued} jobs queued (repo cap {r.max})
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
      <StopRunnerDialog instance={stopping} onClose={() => setStopping(null)} />
    </>
  )
}

export function StopRunnerDialog({ instance, onClose }: { instance: InstanceStatus | null; onClose: () => void }) {
  const queryClient = useQueryClient()
  const status = useStatus()
  // Holds the runner while the dialog animates closed, and reads its live
  // state so the wording follows a runner that picks up a job meanwhile.
  const [held, setHeld] = useState<InstanceStatus | null>(instance)
  if (instance !== null && instance.id !== held?.id) setHeld(instance)
  const live = held ? (status.data?.instances.find((i) => i.id === held.id) ?? held) : null
  const stop = useMutation({
    mutationFn: (id: string) => api.stopRunner(id),
    onSuccess: (_data, id) => {
      toast.success(`stopped ${id}`)
    },
    onSettled: () => queryClient.invalidateQueries({ queryKey: keys.status }),
  })
  const busy = live?.state === "busy"
  return (
    <AlertDialog
      open={instance !== null}
      onOpenChange={(open) => {
        if (!open) onClose()
      }}
    >
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{busy ? `Runner ${live?.id} is running a job. Stop it?` : `Stop runner ${live?.id}?`}</AlertDialogTitle>
          <AlertDialogDescription>
            {busy ? "The job it is running fails." : "ghr stops the runner and cleans it up."}
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>Cancel</AlertDialogCancel>
          <AlertDialogAction
            data-variant="destructive"
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

- [ ] **Step 4: Run the checks**

Run: `timeout 300 npm --prefix web test -- src/pages/runners.test.tsx src/pages/runner-detail.test.tsx src/pages/dashboard.test.tsx`, then the four web checks.
Expected: all PASS. The runner detail page's header button keeps the name `Stop runner`, and its dialog test still passes. If lint reports the render-time `setHeld` as an error, use the fallback Task 3 describes for `ConfirmDialog`.

- [ ] **Step 5: Commit**

```bash
git add web/src/components/runners-table.tsx web/src/pages/runners.test.tsx
git commit -m "fix(web): keep the Stop dialog on its live runner"
```

### Task 20: web: log follow, glyph labels and job icons

**Files:**
- Create: `web/src/components/glyph.tsx`
- Modify: `web/src/components/log-view.tsx`, `web/src/pages/runner-detail.tsx`, `web/src/pages/dashboard.tsx`
- Test: `web/src/components/glyph.test.tsx`, `web/src/components/log-view.test.tsx`, `web/src/pages/runner-detail.test.tsx`, `web/src/pages/dashboard.test.tsx`

**Interfaces:**
- Consumes: C1.
- Produces: C6's `Glyph`. `LogView({ text, follow, onFollowChange? })` gains the optional `onFollowChange: (follow: boolean) => void`.

**Items:** 18
**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 2 - spec 0 - coupling 1 - risk 1 = 4

- [ ] **Step 1: Write the failing tests**

Create `web/src/components/glyph.test.tsx`:

```tsx
import { render, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import { Glyph } from "./glyph"

describe("Glyph", () => {
  it("names the symbol for assistive technology", () => {
    render(<Glyph symbol="✔" label="succeeded" className="text-green-600" />)
    const glyph = screen.getByRole("img", { name: "succeeded" })
    expect(glyph).toHaveTextContent("✔")
    expect(glyph).toHaveClass("text-green-600")
  })
})
```

Replace `web/src/components/log-view.test.tsx` with:

```tsx
import { fireEvent, render } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"
import { LogView } from "./log-view"

let sets: number[] = []
let top = 0

afterEach(() => {
  sets = []
  top = 0
  Reflect.deleteProperty(HTMLElement.prototype, "scrollHeight")
  Reflect.deleteProperty(HTMLElement.prototype, "clientHeight")
  Reflect.deleteProperty(HTMLElement.prototype, "scrollTop")
})

function trackScroll() {
  Object.defineProperty(HTMLElement.prototype, "scrollHeight", { configurable: true, get: () => 500 })
  Object.defineProperty(HTMLElement.prototype, "clientHeight", { configurable: true, get: () => 100 })
  Object.defineProperty(HTMLElement.prototype, "scrollTop", {
    configurable: true,
    get: () => top,
    set: (v: number) => {
      sets.push(v)
      top = v
    },
  })
}

describe("LogView", () => {
  it("keeps the view at the end while following", () => {
    trackScroll()
    const { rerender } = render(<LogView text="one" follow />)
    rerender(<LogView text={"one\ntwo"} follow />)
    expect(sets).toEqual([500, 500])
  })

  it("leaves the scroll alone when not following", () => {
    trackScroll()
    const { rerender } = render(<LogView text="one" follow={false} />)
    rerender(<LogView text={"one\ntwo"} follow={false} />)
    expect(sets).toEqual([])
  })

  it("stops following when the user scrolls up, and follows again at the end", () => {
    trackScroll()
    const onFollowChange = vi.fn()
    const { container, rerender } = render(<LogView text="one" follow onFollowChange={onFollowChange} />)
    const pre = container.querySelector("pre")
    if (!pre) throw new Error("no log view")
    top = 100
    fireEvent.scroll(pre)
    expect(onFollowChange).toHaveBeenLastCalledWith(false)
    rerender(<LogView text="one" follow={false} onFollowChange={onFollowChange} />)
    top = 400
    fireEvent.scroll(pre)
    expect(onFollowChange).toHaveBeenLastCalledWith(true)
    expect(onFollowChange).toHaveBeenCalledTimes(2)
  })
})
```

In `web/src/pages/runner-detail.test.tsx`, change `import { describe, expect, it } from "vitest"` to `import { afterEach, describe, expect, it } from "vitest"` and `import { screen, waitFor, within } from "@testing-library/react"` to `import { fireEvent, screen, waitFor, within } from "@testing-library/react"`. Add this after the `detailRoutes` constant:

```ts
afterEach(() => {
  Reflect.deleteProperty(HTMLElement.prototype, "scrollHeight")
  Reflect.deleteProperty(HTMLElement.prototype, "clientHeight")
  Reflect.deleteProperty(HTMLElement.prototype, "scrollTop")
})
```

Then append these tests inside `describe("runner detail page", …)`:

```ts
  it("names the step icons, with their own for skipped and cancelled", async () => {
    mockApi(
      detailRoutes({
        "GET /api/runners/aaaaaa/steps": [
          { number: 1, name: "Set up job", status: "completed", conclusion: "success" },
          { number: 2, name: "Run tests", status: "in_progress", conclusion: "" },
          { number: 3, name: "Lint", status: "completed", conclusion: "skipped" },
          { number: 4, name: "Deploy", status: "completed", conclusion: "cancelled" },
          { number: 5, name: "Upload", status: "completed", conclusion: "failure" },
          { number: 6, name: "Post checkout", status: "queued", conclusion: "" },
        ],
      }),
    )
    renderApp("/runners/aaaaaa")
    expect(await screen.findByRole("img", { name: "succeeded" })).toHaveTextContent("✔")
    expect(screen.getByText("running")).toHaveClass("sr-only")
    expect(screen.getByRole("img", { name: "skipped" })).toHaveTextContent("–")
    expect(screen.getByRole("img", { name: "cancelled" })).toHaveTextContent("⊘")
    expect(screen.getByRole("img", { name: "failed" })).toHaveTextContent("✖")
    expect(screen.getByRole("img", { name: "pending" })).toHaveTextContent("○")
  })

  it("turns Follow off when the log is scrolled up", async () => {
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
    mockApi(detailRoutes())
    renderApp("/runners/aaaaaa?tab=log")
    const log = await screen.findByText(/Listening for Jobs/)
    expect(screen.getByRole("switch", { name: "Follow" })).toHaveAttribute("aria-checked", "true")
    top = 0
    fireEvent.scroll(log)
    await waitFor(() => expect(screen.getByRole("switch", { name: "Follow" })).toHaveAttribute("aria-checked", "false"))
  })
```

In `web/src/pages/dashboard.test.tsx`, in the test `"shows repos, runners and the activity feed"`, replace the line `expect(screen.getByText(/✔ #41 build/)).toBeInTheDocument()` with:

```ts
    expect(screen.getByText(/#41 build/)).toBeInTheDocument()
```

Then append these tests inside `describe("Dashboard page", …)`:

```ts
  it("names the status glyphs", async () => {
    mockApi(authedRoutes())
    renderApp("/")
    expect(await screen.findByRole("img", { name: "succeeded" })).toHaveTextContent("✔")
    expect(screen.getByRole("img", { name: "queued" })).toHaveTextContent("⧗")
    expect((await screen.findAllByRole("img", { name: "warning" }))[0]).toHaveTextContent("⚠")
  })

  it("marks a cancelled last job with its own icon", async () => {
    const repos = fixtures.status.repos.map((r) => (r.last_job ? { ...r, last_job: { ...r.last_job, conclusion: "cancelled" } } : r))
    mockApi(authedRoutes({ "GET /api/status": { ...fixtures.status, repos } }))
    renderApp("/")
    expect(await screen.findByRole("img", { name: "cancelled" })).toHaveTextContent("⊘")
  })
```

- [ ] **Step 2: Run them to see them fail**

Run: `timeout 300 npm --prefix web test -- src/components/glyph.test.tsx src/components/log-view.test.tsx src/pages/runner-detail.test.tsx src/pages/dashboard.test.tsx`
Expected: FAIL. `./glyph` cannot be resolved, `onFollowChange` is never called, and no element has role `img` with the names asked for.

- [ ] **Step 3: Implement the glyph and the following log**

Create `web/src/components/glyph.tsx`:

```tsx
export function Glyph({ symbol, label, className }: { symbol: string; label: string; className?: string }) {
  return (
    <span role="img" aria-label={label} className={className}>
      {symbol}
    </span>
  )
}
```

Replace `web/src/components/log-view.tsx` with:

```tsx
import { useEffect, useRef } from "react"

const NEAR_END = 8

export function LogView({
  text,
  follow,
  onFollowChange,
}: {
  text: string
  follow: boolean
  onFollowChange?: (follow: boolean) => void
}) {
  const ref = useRef<HTMLPreElement>(null)
  useEffect(() => {
    const el = ref.current
    if (follow && el) el.scrollTop = el.scrollHeight
  }, [text, follow])

  // Scrolling away from the end pauses Follow, and scrolling back resumes it,
  // so reading earlier output is not yanked down by the next poll.
  function onScroll() {
    const el = ref.current
    if (!el || !onFollowChange) return
    const atEnd = el.scrollHeight - el.scrollTop - el.clientHeight <= NEAR_END
    if (atEnd !== follow) onFollowChange(atEnd)
  }

  return (
    <pre
      ref={ref}
      onScroll={onScroll}
      className="h-[60vh] overflow-auto whitespace-pre-wrap rounded-md border bg-muted/40 p-3 font-mono text-xs"
    >
      {text || "no log output yet"}
    </pre>
  )
}
```

- [ ] **Step 4: Label the runner detail's step icons and wire Follow**

In `web/src/pages/runner-detail.tsx`, add `import { Glyph } from "@/components/glyph"` after the `LogView` import, and replace `stepIcon` with:

```tsx
function stepIcon(s: Step) {
  if (s.status === "in_progress") {
    return (
      <span className="inline-flex items-center">
        <Spinner size="sm" />
        <span className="sr-only">running</span>
      </span>
    )
  }
  if (s.conclusion === "success") return <Glyph symbol="✔" label="succeeded" className="text-green-600" />
  if (s.conclusion === "failure") return <Glyph symbol="✖" label="failed" className="text-destructive" />
  if (s.conclusion === "cancelled") return <Glyph symbol="⊘" label="cancelled" className="text-muted-foreground" />
  if (s.conclusion === "skipped") return <Glyph symbol="–" label="skipped" className="text-muted-foreground" />
  return <Glyph symbol="○" label="pending" className="text-muted-foreground" />
}
```

Replace `<LogView text={log.data?.text ?? ""} follow={follow} />` with `<LogView text={log.data?.text ?? ""} follow={follow} onFollowChange={setFollow} />`.

- [ ] **Step 5: Label the Dashboard's glyphs**

In `web/src/pages/dashboard.tsx`:
- change `import type { GhrEvent, Metrics, RepoStatus, Status } from "@/api/types"` to `import type { GhrEvent, HistoryEntry, Metrics, RepoStatus, Status } from "@/api/types"`;
- add `import { Glyph } from "@/components/glyph"` after the `RunnersTable` import;
- add after `repoState`:

```tsx
const jobGlyph: Record<string, { symbol: string; label: string; colour: string }> = {
  success: { symbol: "✔", label: "succeeded", colour: "text-green-600" },
  failure: { symbol: "✖", label: "failed", colour: "text-destructive" },
  cancelled: { symbol: "⊘", label: "cancelled", colour: "text-muted-foreground" },
  skipped: { symbol: "–", label: "skipped", colour: "text-muted-foreground" },
}

function LastJob({ job, now }: { job: HistoryEntry; now: number }) {
  const g = jobGlyph[job.conclusion] ?? jobGlyph.failure
  return (
    <>
      <Glyph symbol={g?.symbol ?? "✖"} label={g?.label ?? "failed"} className={g?.colour} />
      {` #${job.run_number} ${job.job_name}  ${ago(now - Date.parse(job.finished_at))}`}
    </>
  )
}
```

- in `RepoTable`, replace the queue cell `<TableCell>{r.queued > 0 ? <span className="text-amber-600">⧗ {r.queued}</span> : "–"}</TableCell>` with:

```tsx
            <TableCell>
              {r.queued > 0 ? (
                <span className="text-amber-600">
                  <Glyph symbol="⧗" label="queued" /> {r.queued}
                </span>
              ) : (
                "–"
              )}
            </TableCell>
```

- in `RepoTable`, replace the last-job expression (the `{r.last_job ? \`${r.last_job.conclusion === "success" ? "✔" : "✖"} …\` : "–"}` block, lines 132-134) with `{r.last_job ? <LastJob job={r.last_job} now={now} /> : "–"}`;
- replace `const eventIcon: Record<string, string> = { ok: "✔", warn: "⚠", error: "✖" }` with:

```tsx
const eventIcon: Record<string, string> = { ok: "✔", warn: "⚠", error: "✖" }
const eventLabel: Record<string, string> = { ok: "ok", warn: "warning", error: "error" }
```

- in `ActivityFeed`, replace `<span className={eventColour[e.level] ?? "text-primary"}>{eventIcon[e.level] ?? "▶"}</span>` with:

```tsx
            <Glyph symbol={eventIcon[e.level] ?? "▶"} label={eventLabel[e.level] ?? "info"} className={eventColour[e.level] ?? "text-primary"} />
```

- [ ] **Step 6: Run the checks**

Run: `timeout 300 npm --prefix web test -- src/components/glyph.test.tsx src/components/log-view.test.tsx src/pages/runner-detail.test.tsx src/pages/dashboard.test.tsx`, then the four web checks.
Expected: all PASS.

- [ ] **Step 7: Commit**

```bash
git add web/src/components/glyph.tsx web/src/components/glyph.test.tsx web/src/components/log-view.tsx web/src/components/log-view.test.tsx web/src/pages/runner-detail.tsx web/src/pages/runner-detail.test.tsx web/src/pages/dashboard.tsx web/src/pages/dashboard.test.tsx
git commit -m "feat(web): label glyphs and pause Follow on scroll"
```

### Task 21: Go: degraded fixture and CI font guard

**Files:**
- Modify: `web/fixtures_test.go`, `.github/workflows/ci.yml`
- Create (generated): `web/src/api/fixtures/status-degraded.json`
- Test: `web/src/api/types.test.ts`

**Interfaces:**
- Consumes: nothing.
- Produces: the fixture `web/src/api/fixtures/status-degraded.json` (a `Status` with every optional field the UI reads set).

**Items:** 19
**Implementer:** dr-superpowers:impl-sonnet-low
**Evaluation:** files 1 - spec 0 - coupling 0 - risk 0 = 1

- [ ] **Step 1: Write the failing vitest**

In `web/src/api/types.test.ts`, add `import statusDegradedJson from "./fixtures/status-degraded.json"` after the `import statusJson …` line, add `const degraded: Status = statusDegradedJson` after `const status: Status = statusJson`, and append inside `describe("type fixtures", …)`:

```ts
  // tsc cannot catch a renamed optional field (an imported JSON value gets no
  // excess-property check, and a missing optional key is allowed), so this
  // asserts each one the UI reads is still present.
  it("the optional status fields keep their names", () => {
    expect(degraded.degraded).toBe(true)
    expect(degraded.degraded_reason).toBeTruthy()
    expect(degraded.repos[0]?.error).toBeTruthy()
    expect(degraded.repos[0]?.removing).toBe(true)
    expect(degraded.instances[0]).not.toHaveProperty("job")
    expect(degraded.maintenance.running).toBe(true)
    expect(degraded.runner_update.check_error).toBeTruthy()
    expect(degraded.runner_update.last_error).toBeTruthy()
    expect(degraded.runner_update.running).toBe(true)
    expect(degraded.runner_update.last_outcome).toBe("failed")
  })
```

- [ ] **Step 2: Run it to see it fail**

Run: `timeout 300 npm --prefix web test -- src/api/types.test.ts`
Expected: FAIL, `./fixtures/status-degraded.json` cannot be resolved.

- [ ] **Step 3: Add the Go fixture**

In `web/fixtures_test.go`, add this entry to the map `fixtureValues` returns, after the `"status"` entry:

```go
		"status-degraded": model.Status{
			Now: fixtureTime, Epoch: "lz3k9b", Mode: "all", GlobalMax: 2, Degraded: true,
			DegradedReason: "GitHub rejected the token: 401 Bad credentials", DiskPct: 91,
			Repos: []model.RepoStatus{{Name: "darkmem", Paused: true, Removing: true, Max: 1, Error: "GitHub: not found"}},
			Instances: []model.InstanceStatus{
				{ID: "cccccc", Repo: "darkmem", RunnerName: "ghr-cccccc", State: "starting", Since: at(-10 * time.Second)},
			},
			Maintenance: model.MaintenanceStatus{Running: true, LastStarted: ptr(at(-time.Minute))},
			RunnerUpdate: model.RunnerUpdate{Installed: "2.337.0", Latest: "2.338.0", CheckedAt: ptr(at(-time.Hour)),
				CheckError: "GitHub rate limit", Running: true, LastOutcome: "failed", LastError: "download failed: 502",
				LastFinished: ptr(at(-2 * time.Hour))},
		},
```

Run: `timeout 60 gofmt -w web/fixtures_test.go && timeout 300 go test ./web/`
Expected: FAIL, `web/src/api/fixtures/status-degraded.json is out of date with the Go types; regenerate with: GHR_UPDATE_FIXTURES=1 go test ./web/`.

Run: `GHR_UPDATE_FIXTURES=1 timeout 300 go test ./web/ && timeout 300 go test ./web/`
Expected: both PASS, and `web/src/api/fixtures/status-degraded.json` exists.

- [ ] **Step 4: Make the CI font guard fail when there is nothing to check**

With no CSS file, `grep` exits 2 and `!` turns that into success, so the guard passes without checking anything. In `.github/workflows/ci.yml`, replace the `no inlined fonts` step's `run:` line (only the `test` job has this step) with:

```yaml
        run: "ls dist/assets/*.css > /dev/null && ! grep -q 'data:font' dist/assets/*.css"
```

Verify both outcomes locally after a build:

```bash
timeout 600 npm --prefix web run build
(cd web && bash -ec "ls dist/assets/*.css > /dev/null && ! grep -q 'data:font' dist/assets/*.css"); echo "built: $?"
(cd internal && bash -ec "ls dist/assets/*.css > /dev/null 2>&1 && ! grep -q 'data:font' dist/assets/*.css"); echo "missing: $?"
git status --porcelain web/dist
```
Expected: `built: 0`, `missing: 2`, and no `web/dist` changes. Then run `timeout 300 go run github.com/rhysd/actionlint/cmd/actionlint@v1.7.7 .github/workflows/ci.yml`. Expected: no output.

- [ ] **Step 5: Run the checks**

Run the Go checks and the four web checks from Global Constraints.
Expected: all PASS.

- [ ] **Step 6: Commit**

```bash
git add web/fixtures_test.go web/src/api/fixtures/status-degraded.json web/src/api/types.test.ts .github/workflows/ci.yml
git commit -m "test(web): guard optional status fields and fonts"
```

### Task 22: web: test timing, cleanup and navigation retry

**Files:**
- Test: `web/src/api/hooks.test.tsx`, `web/src/pages/runners.test.tsx`, `web/src/pages/history.test.tsx`, `web/src/router.test.tsx`

**Interfaces:**
- Consumes: C4 (a pending handler holds a request open), C1 (`authedRoutes`, `fixtures`, `renderApp`), Task 19's `runners.test.tsx`.
- Produces: nothing.

**Items:** 19
**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 2 - spec 0 - coupling 1 - risk 0 = 3

These tests cover behaviour that already works, so they pass on their first run. Step 2 proves that two of them can fail.

- [ ] **Step 1: Give the real-timer hook tests headroom**

Replace `web/src/api/hooks.test.tsx` with:

```tsx
import { renderHook, waitFor } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import { mockApi } from "@/test/api"
import { withQuery } from "@/test/query"
import { appendEvents, appendLog, MAX_EVENTS, MAX_LOG, useEvents, useLogTail } from "./hooks"
import type { GhrEvent, LogChunk } from "./types"

const ev = (seq: number): GhrEvent => ({ seq, time: "2026-10-03T14:05:00Z", level: "info", msg: `event ${seq}` })

describe("appendEvents", () => {
  it("appends only events newer than the last one held", () => {
    expect(appendEvents([ev(1), ev(2)], [ev(2), ev(3)]).map((e) => e.seq)).toEqual([1, 2, 3])
  })

  it("keeps the last 200", () => {
    const prev = Array.from({ length: MAX_EVENTS }, (_, i) => ev(i + 1))
    const out = appendEvents(prev, [ev(201), ev(202)])
    expect(out).toHaveLength(MAX_EVENTS)
    expect(out[0]?.seq).toBe(3)
    expect(out.at(-1)?.seq).toBe(202)
  })
})

describe("appendLog", () => {
  it("appends the data, advances the cursor and caps the text", () => {
    expect(appendLog({ text: "a", next: "" }, { data: "b", next: "c1" })).toEqual({ text: "ab", next: "c1" })
    expect(appendLog({ text: "x".repeat(MAX_LOG), next: "c1" }, { data: "yz", next: "c2" }).text).toHaveLength(MAX_LOG)
  })
})

describe("useEvents", () => {
  it(
    "polls by cursor and starts over when the epoch changes",
    async () => {
      let restarted = false
      const { calls } = mockApi({
        "GET /api/events": ({ url }: { url: URL }) => {
          const after = Number(url.searchParams.get("after"))
          if (restarted) return after === 0 ? [ev(1)] : []
          if (after === 0) return [ev(1), ev(2)]
          return after === 2 ? [ev(3)] : []
        },
      })
      const { wrapper } = withQuery()
      const { result, rerender } = renderHook(({ epoch }: { epoch: string }) => useEvents(epoch), {
        wrapper,
        initialProps: { epoch: "a" },
      })
      await waitFor(() => expect(result.current.map((e) => e.seq)).toEqual([1, 2, 3]), { timeout: 4000 })
      restarted = true
      rerender({ epoch: "b" })
      await waitFor(() => expect(result.current.map((e) => e.seq)).toEqual([1]), { timeout: 4000 })
      expect(calls.filter((c) => c.search === "?after=0")).toHaveLength(2)
    },
    { timeout: 10_000 },
  )

  it(
    "waits for an epoch before polling",
    async () => {
      const { calls } = mockApi({ "GET /api/events": [] })
      const { wrapper } = withQuery()
      renderHook(() => useEvents(undefined), { wrapper })
      await new Promise((r) => setTimeout(r, 1200))
      expect(calls).toHaveLength(0)
    },
    { timeout: 10_000 },
  )
})

describe("useLogTail", () => {
  it(
    "advances the cursor on every poll",
    async () => {
      const chunks: Record<string, LogChunk> = {
        "": { data: "one\n", next: "c1" },
        c1: { data: "two\n", next: "c2" },
        c2: { data: "", next: "c2" },
      }
      const { calls } = mockApi({
        "GET /api/runners/aaaaaa/log": ({ url }: { url: URL }) => chunks[url.searchParams.get("cursor") ?? ""],
      })
      const { wrapper } = withQuery()
      const { result } = renderHook(() => useLogTail("aaaaaa", true), { wrapper })
      await waitFor(() => expect(result.current.data?.text).toBe("one\ntwo\n"), { timeout: 4000 })
      await new Promise((r) => setTimeout(r, 1200))
      expect(result.current.data?.text).toBe("one\ntwo\n")
      expect(calls.map((c) => c.search).slice(0, 2)).toEqual(["?cursor=", "?cursor=c1"])
    },
    { timeout: 10_000 },
  )

  it(
    "does not poll while the log is closed",
    async () => {
      const { calls } = mockApi({ "GET /api/runners/aaaaaa/log": { data: "x", next: "c1" } })
      const { wrapper } = withQuery()
      renderHook(() => useLogTail("aaaaaa", false), { wrapper })
      await new Promise((r) => setTimeout(r, 1200))
      expect(calls).toHaveLength(0)
    },
    { timeout: 10_000 },
  )
})
```

- [ ] **Step 2: Clean up the clipboard stub even when the copy test fails**

In `web/src/pages/runners.test.tsx`:
- change `import { describe, expect, it, vi } from "vitest"` to `import { afterEach, describe, expect, it, vi } from "vitest"`;
- add after the imports:

```ts
const secureContext = Object.getOwnPropertyDescriptor(window, "isSecureContext")

afterEach(() => {
  Reflect.deleteProperty(navigator, "clipboard")
  if (secureContext) Object.defineProperty(window, "isSecureContext", secureContext)
  else Reflect.deleteProperty(window, "isSecureContext")
})
```

- in the test `"copies a runner ID"`, delete its last line, `Reflect.deleteProperty(navigator, "clipboard")`.

Append to `web/src/pages/history.test.tsx`, inside `describe("History page", …)`:

```ts
  it("shows loading until the first read answers", async () => {
    mockApi(authedRoutes({ "GET /api/history": () => new Promise(() => {}) }))
    renderApp("/history")
    expect(await screen.findByText("loading…")).toBeInTheDocument()
    expect(screen.queryByText("no finished jobs yet")).toBeNull()
  })
```

In `web/src/router.test.tsx`, add `import { authedRoutes } from "@/test/fixtures"` to the imports if the file does not import it yet. Then add, inside `describe("routes", …)` after the test `"offers a retry when the daemon cannot be asked who is logged in"`:

```ts
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

Check that the two new tests can fail. In `web/src/pages/history.tsx`, temporarily replace `history.isPending ?` with `false ?`, and run `timeout 300 npm --prefix web test -- src/pages/history.test.tsx`. Expected: the loading test FAILS. Revert with `git checkout web/src/pages/history.tsx`. Then, in `web/src/components/route-error.tsx`, temporarily replace `void router.invalidate()` with `undefined`, and run `timeout 300 npm --prefix web test -- src/router.test.tsx`. Expected: both retry tests FAIL. Revert with `git checkout web/src/components/route-error.tsx`.

- [ ] **Step 3: Run the checks**

Run: `timeout 300 npm --prefix web test -- src/api/hooks.test.tsx src/pages/runners.test.tsx src/pages/history.test.tsx src/router.test.tsx`, then the four web checks. Afterwards, `git status --porcelain web/src/pages/history.tsx web/src/components/route-error.tsx` must print nothing.
Expected: all PASS. If the sidebar renders two links named `History` (for example a collapsed rail), click the first: `screen.getAllByRole("link", { name: "History" })[0]`.

- [ ] **Step 4: Commit**

```bash
git add web/src/api/hooks.test.tsx web/src/pages/runners.test.tsx web/src/pages/history.test.tsx web/src/router.test.tsx
git commit -m "test(web): harden timing, cleanup and retry tests"
```

### Task 23: homelab: web UI docs

**Files:**
- Modify: `D:/Repositories/Personal/homelab/github-runner/config.example.yaml`, `D:/Repositories/Personal/homelab/github-runner/README.md`

**Interfaces:**
- Consumes: nothing.
- Produces: nothing.

**Items:** 15
**Implementer:** dr-superpowers:impl-sonnet-low
**Evaluation:** files 1 - spec 0 - coupling 0 - risk 0 = 1

This task edits the homelab repository, not the ghr worktree. Homelab commits go on its `master`, as earlier `github-runner` changes did (for example `52bae1a`).

- [ ] **Step 1: Add the commented web block**

In `D:/Repositories/Personal/homelab/github-runner/config.example.yaml`, insert these lines between the `runner_limits:` block (ending `  cpu_quota: 200%`) and `repos:`:

```yaml
# web:                        # browser UI; off until listen is set, then restart ghr
#   listen: 0.0.0.0:8080
#   hosts: [ghr.lan]          # names a proxy serves it under; IPs and localhost always work
```

- [ ] **Step 2: Add the README section**

In `D:/Repositories/Personal/homelab/github-runner/README.md`, insert this section before `## Migrating from the compose runners`:

````markdown
## Web UI

ghr can serve a browser UI with the same pages and actions as `ghr tui`. It stays off until `web.listen` is set.

1. Add a `web:` block to `/etc/ghr/config.yaml` (`config.example.yaml` has one, commented out), set `listen` (for example `0.0.0.0:8080`), and run `systemctl restart ghr`. A reload does not start or move the listener; it only warns `web settings changed; restart ghr to apply`.
2. Open `http://<lxc-ip>:8080` right away and choose the password (12 to 1024 bytes). Until a password is set, the first visitor to reach the port sets it. `ghr web reset-password` deletes the password and logs every browser out, which reopens that window, so log in again right after running it.
3. To reach ghr through nginx-proxy-manager under a name, add that name to `web.hosts` and restart. ghr answers only IP addresses, `localhost` and the listed names, which blocks DNS rebinding. The proxy must forward the original `Host` header, as nginx-proxy-manager does by default.
4. On the LAN the UI is plain HTTP: the password and the session cookie cross the network unencrypted. Put nginx-proxy-manager in front for TLS, and bind `listen` to a LAN-only address.
5. Behind the proxy every browser shares the proxy's address, so five wrong passwords in 15 minutes lock everyone out for 15 minutes. Put an nginx-proxy-manager access list in front of the host.
6. Sessions live in memory: every restart, including each `setup.sh` upgrade, logs you out.

```yaml
web:
  listen: 0.0.0.0:8080
  hosts: [ghr.lan]
```
````

- [ ] **Step 3: Check and commit**

```bash
cd D:/Repositories/Personal/homelab
git diff -- github-runner/config.example.yaml github-runner/README.md
timeout 120 bash github-runner/tests/setup_test.sh
git add github-runner/config.example.yaml github-runner/README.md
git commit -m "docs(github-runner): document the ghr web UI"
```
Expected: the diff shows only the two insertions, and `setup_test.sh` passes as before. If it cannot run on Windows, say so and rely on the diff: the inserted YAML lines are comments.

### Task 24: Manual acceptance on the runner LXC

**Files:**
- Modify: `D:/Repositories/Personal/homelab/docs/superpowers/registers/2026-10-05-ghr-web-ui.md` (through `scripts/register`)

**Interfaces:**
- Consumes: Tasks 1-23 on `feat/web-pages`.
- Produces: register rows 2, 5, 7 and 16 closed, with evidence.

**Items:** 16
**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 0 - spec 1 - coupling 1 - risk 2 = 4

This task is run with the owner, who has access to the LXC and the browser. It also closes register rows 2, 5 and 7, which were waiting on a live daemon. It ends before any push: pushing ghr master publishes a release and needs the owner's explicit yes, and dr-superpowers:finishing-a-development-branch decides how `feat/web-pages` lands.

- [ ] **Step 1: Build what the release job would build**

The flags mirror the `release` job in `.github/workflows/ci.yml`: `CGO_ENABLED=0`, `GOOS=linux`, `GOARCH=amd64`, `-trimpath -ldflags "-s -w -X main.version=…"`, with the UI built first.

```bash
cd D:/Repositories/Personal/ghr-web-pages
timeout 600 npm --prefix web ci && timeout 600 npm --prefix web run build && test -f web/dist/index.html
mkdir -p ../ghr-accept
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 timeout 600 go build -trimpath -ldflags "-s -w -X main.version=v0.1.13-web-accept" -o ../ghr-accept/ghr ./cmd/ghr
git status --porcelain web/dist
```
Expected: `../ghr-accept/ghr` exists, and `git status` prints nothing for `web/dist`.

- [ ] **Step 2: Install it on 192.168.0.99 (owner)**

Copy `../ghr-accept/ghr` to the LXC over the owner's usual access, for example `scp ../ghr-accept/ghr root@192.168.0.99:/tmp/ghr`. Then, on the LXC as root, while `ghr status` shows no job running:

```bash
cp /usr/local/bin/ghr /usr/local/bin/ghr.before-web
install -m 0755 /tmp/ghr /usr/local/bin/ghr        # what setup.sh's install_ghr does
```

Add the web block to `/etc/ghr/config.yaml`. Set `hosts` to the nginx-proxy-manager hostname the owner will use:

```yaml
web:
  listen: 0.0.0.0:8080
  hosts: [<proxy hostname>]
```

Then run `systemctl restart ghr && systemctl --no-pager status ghr`. Expected: active (running), with no bind error in `journalctl -u ghr -n 30`.
Rollback, if anything below fails badly: `install -m 0755 /usr/local/bin/ghr.before-web /usr/local/bin/ghr && systemctl restart ghr`.

- [ ] **Step 3: Run the checks with the owner, in a browser with DevTools open**

Record a pass or fail for each check, and evidence (what was seen):

1. **Setup and login (spec check 1; row 5).** Opening `http://192.168.0.99:8080` shows the setup form. Setting the password lands on the Dashboard. Log out from the header menu: the login page shows. A wrong password shows `wrong password` inline. The right password opens the Dashboard.
2. **Embedded UI, no CSP violations (spec check 4; row 2).** The page is the UI, not `ghr web UI was not built into this binary`. The console shows no `Content-Security-Policy` violation. The Network tab shows no request to another origin.
3. **Dashboard live (row 7).** The status chips, tiles, runners, queue and activity feed update without reloading. Pause all toasts `paused all repos (drain)` and the repos show paused. Resume all toasts `resumed all repos`.
4. **Live job (spec check 2).** Re-run a workflow of a configured repo, for example darkmem. The runner appears on Runners. Its detail page shows the steps advancing and the log tail growing with Follow on. Scrolling the log up turns Follow off.
5. **Repo pause, password change and reset (spec check 3).** On Repositories, pause a repo, then resume it. On Settings, change the password, log out, and log in with the new one. On the LXC, run `ghr web reset-password`: the next page load shows setup. Set the password again.
6. **The new pages.**
   - Repositories: open a repo and run the label check; it finishes.
   - Storage: press Refresh; `measuring…` turns into a new `measured HH:MM`.
   - Settings: change `Poll interval` from `10s` to `15s` and save (`Settings saved`), then set it back.
7. **Through nginx-proxy-manager (spec check 5).** On the proxy hostname, log in and do one write (pause, then resume a repo).

- [ ] **Step 4: Record the outcome in the register**

If every check passed, from `D:/Repositories/Personal/homelab`:

```bash
P=D:/Repositories/Personal/darkraise-ai-plugins/plugins/dr-superpowers
R=docs/superpowers/registers/2026-10-05-ghr-web-ui.md
bash $P/scripts/register set $R 2 done --note "acceptance build v0.1.13-web-accept on 192.168.0.99 served the UI, no CSP violations (<date>)"
bash $P/scripts/register set $R 5 done --note "setup, logout, wrong password inline and login against the live daemon (<date>)"
bash $P/scripts/register set $R 7 done --note "Dashboard live; Pause all and Resume all acted (<date>)"
bash $P/scripts/register set $R 16 done --note "spec §8 checks 1-5 passed on 192.168.0.99, incl. nginx-proxy-manager (<date>)"
for i in 11 12 13 14 15 17 18 19; do bash $P/scripts/register set $R $i done --note "part 3 plan; checked on 192.168.0.99 (<date>)"; done
bash $P/scripts/register check $R
git add $R && git commit -m "docs(registers): record ghr web UI acceptance"
```

Replace `<date>` with the absolute date. If a check fails, do not mark its row done. Set the row's note to the failure, roll back if the daemon is affected, and take the failure to dr-superpowers:systematic-debugging before the branch is finished.

- [ ] **Step 5: Stop before the push**

Leave the acceptance binary running and `/usr/local/bin/ghr.before-web` in place, and tell the owner both. The next step is dr-superpowers:finishing-a-development-branch. Pushing ghr master, which publishes the release that `setup.sh` then installs, happens only on the owner's explicit yes.
