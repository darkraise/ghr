# ghr web UI, part 2: frontend foundation and live pages Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: the skill the **Execution:** line names — dr-superpowers:subagent-driven-development for `subagent`, dr-superpowers:executing-plans for `inline`. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the ghr browser frontend's foundation (darkraise-ui scaffold, CSP-safe page, CI build and release embedding, typed API client with Go-generated fixtures, polling hooks, login, app shell) and its live pages (Dashboard, Runners, runner detail, History), so a release binary serves a working UI instead of the 503 text.

**Architecture:** A Vite + React 19 single-page app in `ghr/web/`, scaffolded by `create-darkraise-ui` 6.9.6 and embedded by the existing `web/embed.go`. A `fetch` wrapper (`src/api/client.ts`) talks to the part 1 server (`/auth/*`, `/api/*`); one TanStack Query hook per endpoint polls on the TUI's cadence; TanStack Router (code-based routes) gates every page behind `/auth/state` and renders inside darkraise-ui's `SidebarLayout`. Hand-written TypeScript types are guarded by JSON fixtures that a Go test generates from `internal/model`.

**Tech Stack:** darkraise-ui 6.9.6, React 19, Vite 6, Tailwind 4, TypeScript 5.7, @tanstack/react-router 1.170, @tanstack/react-query 5.104, lucide-react 1.52, vitest 4 + jsdom 29 + Testing Library, ESLint 9; Go 1.26 for the fixture test; GitHub Actions `actions/setup-node@v4`.

**Spec:** docs/superpowers/specs/2026-10-05-ghr-web-ui-design.md

**Execution:** inline — `claude --model sonnet --effort high` — 1 of 19 tasks is heavy (Task 12, total 5: the route tree and auth gate every page depends on) and is delegated; the self-implemented tasks top out at total 4 (`medium`), raised to `high` because a task is delegated.
**Plan review:** 2026-10-06 — dr-superpowers:judge-opus — executability 17 / coherence 17 / coverage 18 / assumptions 17 (round 2)

## Global Constraints

- Code repository: `D:/Repositories/Personal/ghr` (github.com/darkraise/ghr). Work in a new worktree `D:/Repositories/Personal/ghr-web-frontend` on branch `feat/web-frontend` from master `96590f2` (v0.1.12). Never commit to master, never push, never open a PR without the owner's yes: pushing master publishes a release through `ci.yml`. The plan, spec and register live in `D:/Repositories/Personal/homelab`.
- Every command runs from the worktree root in Git Bash with an explicit `timeout`. Web commands use `npm --prefix web …` (npm runs package scripts through `cmd.exe` on Windows; scripts in `web/package.json` must not use POSIX-only syntax such as `VAR=value cmd`).
- Web checks: `timeout 300 npm --prefix web run lint`, `timeout 300 npm --prefix web run typecheck`, `timeout 300 npm --prefix web test`, `timeout 600 npm --prefix web run build`. Lint warnings are allowed; errors are not.
- Go checks before each commit that touches Go: `timeout 120 gofmt -l internal cmd web/*.go` (must print nothing), `timeout 300 go vet ./...`, `timeout 400 go test ./...`. After editing a Go file, run `timeout 60 gofmt -w <file>`. CI's `go test -race` cannot run here (no cgo on Windows).
- Never run `vite` (the dev server), `vitest` without `run`, or any watch mode. Kill any process you start.
- Never commit `web/node_modules/` or files under `web/dist/` other than the existing `web/dist/.gitkeep`; `web/embed.go` (`//go:embed all:dist`) stays unchanged. After `npm run build`, `git status --porcelain web/dist` must print nothing.
- CSP (spec §2, already enforced by the server): `default-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; base-uri 'none'; object-src 'none'; form-action 'self'; frame-ancestors 'none'`. So: no inline `<script>`, no external URLs (fonts come from `@fontsource`), no `eval`.
- Every request carries `X-GHR: 1` and `credentials: "same-origin"` (spec §5 API layer).
- Polling cadence (spec §5, from `internal/tui/model.go`): status and events every 1 s; log tail every 1 s while a log view is open (a finished runner's log stays readable: the daemon serves the archived copy); steps and containers every 5 s on runner detail while the runner is live; config and metrics every 5 s; history every 5 s on History with `limit=200`. Status, events, config and metrics poll on every page because the shell mounts their hooks (Task 14). TanStack's default pauses polling while the tab is hidden. Queries do not retry (`retry: false`): the next poll is the retry, as in the TUI.
- Owner rulings, 2026-10-06 (chat): (1) Runners rows offer **Copy ID** (spec §5), and runner detail and History rows offer an **Open run** link to the job's GitHub page (the TUI's "Copy run URL"); (2) **Stop runner always confirms** (spec §5), with the TUI's busy wording when a job runs; (3) part 2 adds **Log out** to the header's user menu (spec §5 keeps it on Settings too, in part 3).
- UI texts copied from the TUI, verbatim: `no runners — they start when jobs are queued`; `Runner <id> is running a job. Stop it?`; `stopped <id>`; `paused all repos (drain)`; `resumed all repos`; `daemon unreachable: <error> — retrying`; `DEGRADED: <reason> — no new runners`; `This runner has finished`; `no steps reported yet`; `none in this runner's compose projects`; `no finished jobs yet`; `<n> jobs queued (repo cap <max>)`; `copied <text>`.
- Repositories, Storage and Settings stay placeholder pages in part 2 (`Not in the web UI yet`); part 3 builds them.
- Comments: none unless the why is non-obvious; never reference this plan, a task, the spec or a review.
- Commits: `<type>(<scope>): <subject>`, subject ≤ 50 characters, imperative, no period. One commit per task.
- The Codex executor lane is closed until 2026-10-10: Claude seats only, no `**Executor:**` lines.

## Contracts

**C1 scaffold copy (Task 1 → Task 4):** Task 1 runs the scaffolder in `D:/Repositories/Personal/ghr-web-scaffold` (outside both repositories) and copies three files from `ghr-web-scaffold/web/`; Task 4 extracts the theme script from `ghr-web-scaffold/web/index.html` and then deletes `ghr-web-scaffold`.

**C2 `web/` package (Tasks 1-3):** scripts `dev` (`vite`), `build` (`vite build && tsc --noEmit && node -e "require('fs').writeFileSync('dist/.gitkeep','')"`), `typecheck` (`tsc --noEmit`), `lint` (`eslint src`), `test` (`vitest run`). The import alias `@/` maps to `web/src/`. Vitest runs in jsdom with `TZ=UTC` and the setup file `web/src/test/setup.ts`.

**C3 fixtures (Task 6 → Tasks 7+):** `web/src/api/fixtures/<name>.json`, `<name>` ∈ `status`, `events`, `history`, `log`, `steps`, `containers`, `metrics`, `config`, `storage`, `token`, `label-check`, `registrations`, `available-repos`, `toolchain-choices`. Regenerate with `GHR_UPDATE_FIXTURES=1 timeout 300 go test ./web/`. Fixture facts later tests rely on: instances `aaaaaa` (repo `darkmem`, state `busy`, job `test` run `42`, `html_url` set) and `bbbbbb` (repo `darkmem`, `idle`, no job); repos `darkmem` (max 2, active 1, queued 3, last job `build` #41 success), `darkcloud` (paused), `old-repo` (paused, removing, error); `global_max` 2, mode `queue`, `rate_remaining` 4980, `disk_pct` 61, `epoch` `lz3k9a`, `runner_update.latest` `2.338.0` with a deadline; events seq 1-3 (third: `repo paused`, repo `darkcloud`); history entries `build` (darkmem, #41, success, `html_url`) and `deploy` (darkcloud, #7, failure, no URL); steps `Set up job` (completed/success), `Run tests` (in_progress), `Post checkout` (queued); one container `ghr-aaaaaa-db-1`; log data containing `Listening for Jobs`; metrics `cpu` 31, `mem_used` 3 GiB, `mem_total` 16 GiB; config repos `darkmem`, `darkcloud`, `disk_high_water` 80.

**C4 `web/src/api/types.ts` (Task 7):** exported interfaces `AuthState`, `JobInfo`, `InstanceStatus`, `HistoryEntry`, `RepoStatus`, `MaintenanceStatus`, `RunnerUpdate`, `Status`, `GhrEvent`, `Step`, `LogChunk`, `Container`, `MetricSample`, `Metrics`, `RunnerLimits`, `WebConfig`, `RepoConfig`, `Config`, `TokenStatus`, `Registration`, `LabelGroup`, `LabelCheck`, `AvailableRepo`, `Toolchain`, `Folder`, `PackageCache`, `DockerRow`, `BuildCacheType`, `DockerDisk`, `Operation`, `Operations`, `PruneStep`, `LastPrune`, `Storage`, `ToolchainChoice`. `web/src/test/fixtures.ts` exports `fixtures` (`{ status: Status, events: GhrEvent[], history: HistoryEntry[], log: LogChunk, steps: Step[], containers: Container[], metrics: Metrics, config: Config }`) and `authedRoutes(over?: Record<string, unknown>): Record<string, unknown>`, which answers `GET /auth/state` as logged in and `GET /api/status|config|metrics|events` with the fixtures, overridden by `over`.

**C5 `web/src/api/client.ts` (Task 8):**
```ts
export class ApiError extends Error { readonly status: number; readonly retryAt?: Date; constructor(status: number, message: string, retryAt?: Date) }
export function setUnauthorizedHandler(fn: () => void): void
export function request<T>(method: string, path: string, body?: unknown, signal?: AbortSignal): Promise<T>
export const api: {
  authState(signal?): Promise<AuthState>; setup(password): Promise<void>; login(password): Promise<void>
  logout(): Promise<void>; changePassword(current, next): Promise<void>
  status(signal?): Promise<Status>; events(after: number, signal?): Promise<GhrEvent[]>
  history(repo: string, conclusion: string, limit: number, signal?): Promise<HistoryEntry[]>
  log(id: string, cursor: string, signal?): Promise<LogChunk>; steps(id, signal?): Promise<Step[]>
  containers(id, signal?): Promise<Container[]>; metrics(signal?): Promise<Metrics>; config(signal?): Promise<Config>
  stopRunner(id): Promise<void>; pauseAll(): Promise<void>; resumeAll(): Promise<void>
}
```
`web/src/test/api.ts` exports `mockApi(routes: Record<string, unknown>): { calls: Call[]; fetchMock }` (route key `"<METHOD> <path>"`, value a JSON body or a `(req: { url: URL; method: string; body: unknown }) => Response | unknown` handler; unknown routes answer 404 JSON), `json(value, status = 200): Response`, `noContent(): Response`, and `interface Call { method: string; path: string; search: string; body: unknown }`. A route that returns a `Response` must be a handler, because a `Response` body can be read once.

**C6 `web/src/api/hooks.ts` (Task 9):** constants `POLL_FAST = 1000`, `POLL_SLOW = 5000`, `MAX_EVENTS = 200`, `MAX_LOG = 256 * 1024`; `keys` (`auth`, `status`, `config`, `metrics`, `events(epoch)`, `history(repo, conclusion)`, `steps(id)`, `containers(id)`, `log(id)`); hooks `useStatus()`, `useConfig()`, `useMetrics()`, `useHistory(repo, conclusion)`, `useSteps(id, live)`, `useContainers(id, live)` (TanStack `UseQueryResult`s), `useEvents(epoch: string | undefined): GhrEvent[]`, `useLogTail(id, enabled): UseQueryResult<LogTail>`; pure `appendEvents(prev, next)`, `appendLog(prev, chunk)`; `interface LogTail { text: string; next: string }`. `web/src/test/query.tsx` exports `withQuery(): { queryClient; wrapper }`.

**C7 `web/src/lib/format.ts` and `web/src/lib/use-now.ts` (Task 10):** `dur(ms)`, `ago(ms)`, `humanBytes(n)`, `fmtMem(bytes)`, `maxText(n)`, `isZeroTime(iso)`, `clock(iso)` (`HH:MM:SS` local), `hhmm(date)` (`HH:MM` local), `dateTime(iso)` (`YYYY-MM-DD HH:MM` local), `dateTimeSec(iso)`, `elapsed(instance, now)`, `series(samples, pick)`; `useNow(): number` (milliseconds, refreshed every second).

**C8 components (Task 11):** `web/src/components/state-badge.tsx` exports `StateBadge({ state })` and `stateVariant(state): BadgeVariant`; `web/src/components/sparkline.tsx` exports `Sparkline({ values, max?, label })`; `web/src/lib/clipboard.ts` exports `copyText(text): Promise<void>`.

**C9 routes (Task 12):** `web/src/router.tsx` exports `createAppRouter({ queryClient, history? })` and `type AppRouter`, and registers the router type with TanStack. Paths: `/login`, and under the pathless layout route `app`: `/`, `/repositories`, `/runners`, `/runners/$id` (search `{ tab: DetailTab }`, default `steps`), `/history`, `/storage`, `/settings`. Page modules, each exporting one named component: `pages/login.tsx` `LoginPage`, `pages/dashboard.tsx` `DashboardPage`, `pages/runners.tsx` `RunnersPage`, `pages/runner-detail.tsx` `RunnerDetailPage` and `type DetailTab = "steps" | "log" | "containers"`, `pages/history.tsx` `HistoryPage`, `pages/repositories.tsx` `RepositoriesPage`, `pages/storage.tsx` `StoragePage`, `pages/settings.tsx` `SettingsPage`, `pages/not-yet.tsx` `NotYet({ title })`. `web/src/lib/router-adapter.tsx` exports `routerAdapter: RouterAdapter`.

**C10 app (Task 13):** `web/src/query.ts` exports `createQueryClient(): QueryClient` and `errorText(err: unknown): string`; `web/src/app.tsx` exports `App({ router, queryClient })`; `web/src/test/render.tsx` exports `renderApp(path): RenderResult & { router; queryClient; user }`.

**C11 shell (Task 14):** `web/src/components/shell.tsx` exports `Shell`, the `app` route's component; it mounts `useStatus`, `useConfig`, `useMetrics` and `useEvents(epoch)`, so every page test through `renderApp` needs `authedRoutes` (or tolerates 404s for those four); the user menu's trigger button is named `O` (the initial of the user name `Owner`). From Task 14 on, `web/src/router.test.tsx` renders through `renderApp`.

**C12 runners (Task 16):** `web/src/components/runners-table.tsx` exports `RunnersTable({ status, actions })` and `StopRunnerDialog({ instance, onClose })`.

## Assumptions (evidence)

- `create-darkraise-ui@6.9.6` accepts `web --layout sidebar -y --no-git --no-install`, refuses an existing target directory, and writes `src/theme.config.ts`, `src/env.d.ts`, `src/styles/globals.css` (`@import "darkraise-ui/styles.css";`) and an `index.html` whose only inline `<script>` is the theme bootstrap: scaffold run and 6.9.6 source, 2026-10-06 (scratchpad research). It cannot scaffold into ghr's existing `web/` (holds `embed.go`, `.gitignore`, `dist/.gitkeep`), so Task 1 scaffolds outside and copies.
- darkraise-ui 6.9.6 needs no i18next at runtime (`i18next`/`react-i18next` are optional peers; components render without a provider): `node_modules/darkraise-ui/package.json` `peerDependenciesMeta`, scaffold build without them, 2026-10-06.
- darkraise-ui exports used, read from `node_modules/darkraise-ui/dist/**/*.d.ts` 2026-10-06: `darkraise-ui/layout` (`SidebarLayout` with `nav`, `notificationSlot`, `user`, `onLogout`; `PageHeader` renders its title as `<h1>`; `useBrandStore`; types `NavGroup`); `darkraise-ui/router` (`RouterAdapterProvider`, types `RouterAdapter`, `RouterLinkProps`); `darkraise-ui/theme` (`ThemeProvider`); components under `darkraise-ui/components/<name>` (`alert`, `alert-dialog`, `badge` with `BadgeVariant`, `button` with `asChild`, `card`, `empty-state`, `input`, `label`, `select`, `sonner` (`Toaster`, `toast`), `spinner` (`label`), `stat`, `switch`, `table`, `tabs`). `Tabs`, `Select` `onValueChange` and `Switch` `onCheckedChange` take a plain `string`/`boolean`.
- `SidebarLayout` with nav items throws unless a `RouterAdapterProvider` wraps it, and `SidebarNav` renders links with `activeExact`, so `/runners/$id` does not highlight Runners (accepted); `ThemeProvider` calls `window.matchMedia` unguarded, so jsdom needs the setup stub; the user menu trigger shows the user name's initials and its item reads `Log out`: smoke test and `dist/chunk-4S4GXBTH.js` `UserMenu`, 2026-10-06.
- A toast's text appears twice in the DOM (the toast and an aria-live mirror), so tests use `findAllByText` for toasts: jsdom smoke test, 2026-10-06.
- vitest 4.1.11 and jsdom 29.1.1 support Node 24 and the local Node 25.5.0; vitest 5 and jsdom 30 exclude Node 25: `npm view` engines, 2026-10-06. CI pins Node 24 through `web/.nvmrc`.
- ESLint 9 with `@eslint/js` 9, `typescript-eslint` 8.58, `eslint-plugin-react-hooks` 7.0.1 (`configs.flat.recommended`), `eslint-plugin-react-refresh` 0.5.2 and `globals` 17 matches the template's rule set minus Storybook and Prettier: darkraise-web-template `eslint.config.js`, 2026-10-06. Unverified that the react-hooks 7 recommended set raises no error on the plan's code — Task 3 and every later task's lint step verify it; `Date.now()` is never called during render (`useNow` holds it in state).
- `web/node_modules/flatted/golang/pkg/flatted/flatted.go` (an ESLint dependency) has no `go.mod`, so with `web/node_modules` present `go vet ./...` and `go test ./...` include that package; it vets cleanly and is gofmt-clean (standalone `go vet` and `gofmt -l`, 2026-10-06). CI runs the Go steps before `npm ci`, so CI never sees it. Task 1 re-verifies `go vet ./...` after `npm ci`.
- `/api/*` routes and JSON shapes, `/auth/*` bodies and errors, and the JSON encoding rules (zero `time.Time` encodes as `"0001-01-01T00:00:00Z"`; `Storage.last_prune` and `Operations.current` encode `null`; storage lists may be `null`): `internal/api/server.go:90-300`, `internal/webui/handler.go`, `internal/model/model.go`, `internal/model/storage.go`, `internal/config/config.go`, read 2026-10-06.
- TUI behaviour mirrored here (labels, state badge colours, waiting rows, detail snapshot, history filters): `internal/tui/{shell,view,dashboard,runners,detail,history,model}.go`, read 2026-10-06.
- TanStack Query does not run two fetches of one query key at once (a refetch either joins or cancels the running one), so `useEvents` and `useLogTail`, which read their previous data inside `queryFn`, never append one cursor's data twice. Unverified against 5.104's interval refetch — Task 9's `useLogTail` test (exact text after several polls) verifies it.
- With the router type registered, TanStack's `Link` accepts the adapter's plain-string `to` as the template's `RouterLink` does: darkraise-web-template `src/lib/RouterLink.tsx`; Task 12's typecheck verifies it.
- `navigator.clipboard` exists only in secure contexts (HTTPS or localhost) and the LAN listener is plain HTTP, so Copy ID falls back to `document.execCommand("copy")`: MDN Clipboard API (secure context requirement).
- `actions/setup-node@v4` supports `node-version-file`, `cache: npm` and `cache-dependency-path`: https://github.com/actions/setup-node. `rhysd/actionlint` v1.7.7 validates workflows offline: https://github.com/rhysd/actionlint. Task 5 runs it.
- darkraise-ui component facts the tests rely on, read from `dist/**/*.d.ts` and the dist chunks 2026-10-06 and confirmed by the round 1 plan review: `Button` takes `loading` and `asChild`; `Badge` takes the colour names used here (`green`, `blue`, `amber`, `red`, `secondary`, `outline`) and `size="sm"`; `Alert` has a `warning` variant; `Spinner` takes `size` and `label` (rendered visibly); the `Select` trigger has role `combobox` and its items role `option` (its own component, so jsdom's missing pointer capture is not hit); `Switch` renders role `switch` with `aria-checked`; `TabsTrigger` renders role `tab`; `AlertDialogContent` renders role `alertdialog`; `AlertDialogAction` is a dialog close button, so it closes the dialog on click (Task 16's test asserts it), and `data-variant="destructive"` is how the template's products page styles it.
- `SidebarLayout` renders `LayoutHeader`, whose `ThemeSwitcher` calls `useTheme()`, which throws without a `ThemeProvider` (`dist/chunk-4S4GXBTH.js:420,443`, `dist/chunk-BBPBRP6Y.js:2649-2652`). So once the shell renders, every test must render through `renderApp` (`App` holds the `ThemeProvider`); Task 14 rewrites `router.test.tsx` for this.
- A finished runner's log stays readable: `Manager.RunnerLog` falls back to the archived `Paths.Logs/<id>` when the instance directory is gone (`internal/runner/cleanup.go:402-412`), so runner detail keeps polling the log after the runner leaves `/status`, as the TUI does. The round 1 review suggested stopping it; rejected on this evidence.
- Register row 2's acceptance ("a release binary serves index.html instead of the 503 text") is met by the release job's `test -f dist/index.html` before `go build` (Task 5), part 1's embed (`web/embed.go`, `//go:embed all:dist`) and its static-handler tests, and part 3's manual acceptance check 4 (spec §8). No local Go test asserts `index.html` in `web.Dist`: the CI test job builds the UI only after `go test` runs (ci.yml), so such a test would fail there.
- File-shape counting for the Evaluation lines: a test file and the test helpers it introduces count as one shape; a generated file (lockfile, fixtures) counts with the file that generates it.
- Register `docs/superpowers/registers/2026-10-05-ghr-web-ui.md` rows 2-10 are this plan's; rows 11-16 belong to part 3. Row 14 (confirmations) stays with part 3 even though the Stop runner confirmation lands here.

## Task index

1. web: npm package from the scaffold
2. web: compiler and Vite config
3. web: lint and test setup
4. web: CSP-safe page and skeleton app
5. CI: lint, test and build the web UI
6. Go: web type fixtures
7. web: API types
8. web: API client
9. web: polling hooks
10. web: formatting helpers
11. web: state badge, sparkline and clipboard
12. web: routes, auth gate and page stubs
13. web: app providers and test renderer
14. web: shell with banners and log out
15. web: login and setup page
16. web: Runners page
17. web: Dashboard page
18. web: runner detail page
19. web: History page

---

### Task 1: web: npm package from the scaffold

**Files:**
- Create: `web/package.json`, `web/package-lock.json` (generated), `web/.nvmrc`
- Create (copied from the scaffold): `web/src/theme.config.ts`, `web/src/env.d.ts`, `web/src/styles/globals.css`
- Modify: `web/.gitignore`

**Interfaces:**
- Consumes: nothing.
- Produces: C1 (the scaffold directory Task 4 reads), C2 (package scripts).

**Items:** 2

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 0 = 3

- [ ] **Step 1: Create the worktree**

```bash
cd D:/Repositories/Personal/ghr && timeout 60 git worktree add -b feat/web-frontend D:/Repositories/Personal/ghr-web-frontend 96590f2
cd D:/Repositories/Personal/ghr-web-frontend && git log --oneline -1
```
Expected: `96590f2 refactor(daemon): defer closing storage in Run`.

- [ ] **Step 2: Run the scaffolder outside both repositories**

```bash
mkdir -p D:/Repositories/Personal/ghr-web-scaffold && cd D:/Repositories/Personal/ghr-web-scaffold && timeout 600 npx -y create-darkraise-ui@6.9.6 web --layout sidebar -y --no-git --no-install
ls D:/Repositories/Personal/ghr-web-scaffold/web/src
```
Expected: `app.tsx  env.d.ts  main.tsx  styles  theme.config.ts`.

- [ ] **Step 3: Copy the three scaffold files into ghr**

```bash
cd D:/Repositories/Personal/ghr-web-frontend
mkdir -p web/src/styles
cp D:/Repositories/Personal/ghr-web-scaffold/web/src/theme.config.ts web/src/theme.config.ts
cp D:/Repositories/Personal/ghr-web-scaffold/web/src/env.d.ts web/src/env.d.ts
cp D:/Repositories/Personal/ghr-web-scaffold/web/src/styles/globals.css web/src/styles/globals.css
cat web/src/styles/globals.css web/src/env.d.ts
```
Expected: `@import "darkraise-ui/styles.css";` and `/// <reference types="vite/client" />`. Leave `ghr-web-scaffold` in place; Task 4 reads its `index.html`.

- [ ] **Step 4: Write `web/package.json`**

```json
{
  "name": "ghr-web",
  "version": "0.0.0",
  "private": true,
  "type": "module",
  "scripts": {
    "dev": "vite",
    "build": "vite build && tsc --noEmit && node -e \"require('fs').writeFileSync('dist/.gitkeep','')\"",
    "typecheck": "tsc --noEmit",
    "lint": "eslint src",
    "test": "vitest run"
  },
  "dependencies": {
    "@fontsource/inter": "^5.3.0",
    "@fontsource/jetbrains-mono": "^5.3.0",
    "@tanstack/react-query": "^5.104.1",
    "@tanstack/react-router": "^1.170.41",
    "darkraise-ui": "^6.9.6",
    "lucide-react": "^1.52.0",
    "react": "^19.3.0",
    "react-dom": "^19.3.0"
  },
  "devDependencies": {
    "@eslint/js": "^9.39.0",
    "@tailwindcss/vite": "^4.3.3",
    "@testing-library/jest-dom": "^7.0.1",
    "@testing-library/react": "^16.3.3",
    "@testing-library/user-event": "^14.6.7",
    "@types/node": "^22.0.0",
    "@types/react": "^19.0.0",
    "@types/react-dom": "^19.0.0",
    "@vitejs/plugin-react-swc": "^4.3.3",
    "eslint": "^9.39.5",
    "eslint-plugin-react-hooks": "^7.0.1",
    "eslint-plugin-react-refresh": "^0.5.2",
    "globals": "^17.4.0",
    "jsdom": "^29.1.1",
    "tailwindcss": "^4.3.3",
    "typescript": "~5.7.3",
    "typescript-eslint": "^8.58.0",
    "vite": "^6.4.4",
    "vitest": "^4.1.11"
  }
}
```

- [ ] **Step 5: Write `web/.nvmrc` and extend `web/.gitignore`**

`web/.nvmrc`:
```
24
```

`web/.gitignore` (replace the whole file; the first two lines are part 1's and must stay):
```
dist/*
!dist/.gitkeep
node_modules/
*.local
.env
.env.*
!.env.example
```

- [ ] **Step 6: Install and generate the lockfile**

Run: `timeout 900 npm --prefix web install`
Expected: exit 0 and `web/package-lock.json` created. Engine warnings for Node 25 are acceptable; errors are not.

- [ ] **Step 7: Verify the ignores and the Go build still hold**

```bash
git check-ignore -q web/node_modules/react/package.json && echo node_modules-ignored
touch web/dist/probe.txt && git check-ignore -q web/dist/probe.txt && echo dist-ignored; rm web/dist/probe.txt
git status --porcelain
timeout 300 go vet ./... && echo vet-ok
```
Expected: `node_modules-ignored`, `dist-ignored`; `git status` lists only `web/.gitignore`, `web/.nvmrc`, `web/package.json`, `web/package-lock.json`, `web/src/`; `vet-ok` (the vet run now also covers `web/node_modules/flatted/golang/pkg/flatted`, see Assumptions).

- [ ] **Step 8: Commit**

```bash
git add web/.gitignore web/.nvmrc web/package.json web/package-lock.json web/src/theme.config.ts web/src/env.d.ts web/src/styles/globals.css
git commit -m "build(web): add the web UI npm package"
```

---

### Task 2: web: compiler and Vite config

**Files:**
- Create: `web/tsconfig.json`, `web/vite.config.ts`

**Interfaces:**
- Consumes: C2 package (Task 1).
- Produces: C2 (alias `@/`, the Vitest settings Task 3's setup file plugs into, the dev proxy).

**Items:** 2

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 0 = 3

- [ ] **Step 1: Write `web/tsconfig.json`**

```json
{
  "compilerOptions": {
    "target": "ES2022",
    "useDefineForClassFields": true,
    "lib": ["ES2022", "DOM", "DOM.Iterable"],
    "module": "ESNext",
    "skipLibCheck": true,
    "moduleResolution": "bundler",
    "allowImportingTsExtensions": true,
    "isolatedModules": true,
    "moduleDetection": "force",
    "noEmit": true,
    "jsx": "react-jsx",
    "strict": true,
    "noUnusedLocals": true,
    "noUnusedParameters": true,
    "noFallthroughCasesInSwitch": true,
    "noUncheckedIndexedAccess": true,
    "resolveJsonModule": true,
    "paths": { "@/*": ["./src/*"] }
  },
  "include": ["src"]
}
```

- [ ] **Step 2: Write `web/vite.config.ts`**

```ts
import { defineConfig } from "vitest/config"
import react from "@vitejs/plugin-react-swc"
import tailwindcss from "@tailwindcss/vite"
import path from "node:path"

const target = process.env.GHR_DEV_URL ?? "http://localhost:8080"

export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: { alias: { "@": path.resolve(__dirname, "./src") } },
  server: {
    host: "localhost",
    port: 5173,
    // changeOrigin stays false: the daemon must see Host: localhost so its
    // Host allowlist and Origin check pass for the dev server.
    proxy: { "/api": { target }, "/auth": { target } },
  },
  test: {
    environment: "jsdom",
    environmentOptions: { jsdom: { url: "http://localhost/" } },
    env: { TZ: "UTC" },
    setupFiles: ["./src/test/setup.ts"],
    include: ["src/**/*.test.{ts,tsx}"],
  },
})
```

- [ ] **Step 3: Type-check the copied sources**

Run: `timeout 300 npm --prefix web run typecheck`
Expected: exit 0 (it checks `theme.config.ts` and `env.d.ts`; `vite.config.ts` is outside `include` and is exercised by Task 3's test run and Task 4's build).

- [ ] **Step 4: Commit**

```bash
git add web/tsconfig.json web/vite.config.ts
git commit -m "build(web): add TypeScript and Vite config"
```

---

### Task 3: web: lint and test setup

**Files:**
- Create: `web/eslint.config.js`, `web/src/test/setup.ts`

**Interfaces:**
- Consumes: C2 (Tasks 1-2).
- Produces: C2 (the lint rule set and the Vitest setup every test relies on).

**Items:** 2

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 0 = 3

- [ ] **Step 1: Write `web/eslint.config.js`**

```js
import js from "@eslint/js"
import globals from "globals"
import tseslint from "typescript-eslint"
import reactHooks from "eslint-plugin-react-hooks"
import reactRefresh from "eslint-plugin-react-refresh"

export default tseslint.config(
  { ignores: ["dist/**", "node_modules/**"] },
  js.configs.recommended,
  ...tseslint.configs.strict,
  reactHooks.configs.flat.recommended,
  {
    files: ["src/**/*.{ts,tsx}"],
    languageOptions: { globals: { ...globals.browser } },
    plugins: { "react-refresh": reactRefresh },
    rules: {
      "react-hooks/set-state-in-effect": "off",
      "react-hooks/static-components": "off",
      "react-hooks/incompatible-library": "off",
      "react-refresh/only-export-components": ["warn", { allowConstantExport: true }],
    },
  },
  {
    files: ["src/test/**", "src/**/*.test.{ts,tsx}"],
    rules: { "react-refresh/only-export-components": "off" },
  },
)
```

- [ ] **Step 2: Write `web/src/test/setup.ts`**

```ts
import "@testing-library/jest-dom/vitest"
import { cleanup } from "@testing-library/react"
import { afterEach, vi } from "vitest"

// darkraise-ui's ThemeProvider calls window.matchMedia without a guard, and
// jsdom implements neither matchMedia nor scrollTo.
if (!window.matchMedia) {
  Object.defineProperty(window, "matchMedia", {
    writable: true,
    configurable: true,
    value: (query: string) => ({
      matches: false,
      media: query,
      onchange: null,
      addEventListener: () => {},
      removeEventListener: () => {},
      addListener: () => {},
      removeListener: () => {},
      dispatchEvent: () => false,
    }),
  })
}
window.scrollTo = () => {}

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})
```

- [ ] **Step 3: Run lint, typecheck and the (empty) test run**

```bash
timeout 300 npm --prefix web run lint
timeout 300 npm --prefix web run typecheck
timeout 300 npm --prefix web test -- --passWithNoTests
```
Expected: all three exit 0; the test run prints `No test files found` and passes, which proves `vite.config.ts` and the setup file load.

- [ ] **Step 4: Commit**

```bash
git add web/eslint.config.js web/src/test/setup.ts
git commit -m "build(web): add lint and test setup"
```

---

### Task 4: web: CSP-safe page and skeleton app

**Files:**
- Create: `web/index.html`, `web/public/theme-init.js`, `web/public/logo.svg`
- Create: `web/src/main.tsx`, `web/src/app.tsx`
- Test: `web/src/app.test.tsx`

**Interfaces:**
- Consumes: C1 (the scaffold's `index.html`), C2.
- Produces: `web/src/main.tsx` and `web/src/app.tsx`, which Task 13 rewrites; the built `web/dist/index.html`.

**Items:** 2

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 1 = 4

- [ ] **Step 1: Write the failing test `web/src/app.test.tsx`**

```tsx
import { readFileSync } from "node:fs"
import { fileURLToPath } from "node:url"
import { render, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import { App } from "./app"

describe("App", () => {
  it("renders the ghr heading", () => {
    render(<App />)
    expect(screen.getByRole("heading", { name: "ghr" })).toBeInTheDocument()
  })
})

describe("index.html", () => {
  const html = readFileSync(fileURLToPath(new URL("../index.html", import.meta.url)), "utf8")

  it("has no inline script, which the CSP forbids", () => {
    expect(html).not.toMatch(/<script(?![^>]*\bsrc=)[^>]*>/)
  })

  it("loads nothing from another origin", () => {
    expect(html).not.toMatch(/https?:\/\//)
  })

  it("is titled ghr", () => {
    expect(html).toContain("<title>ghr</title>")
  })
})
```

- [ ] **Step 2: Run it to verify it fails**

Run: `timeout 300 npm --prefix web test`
Expected: FAIL — `Failed to resolve import "./app"` (and no `index.html`).

- [ ] **Step 3: Extract the theme script and write the page**

Extract the scaffold's inline theme bootstrap, unchanged, into `web/public/theme-init.js`:
```bash
mkdir -p web/public
node -e "const fs=require('fs');const s=fs.readFileSync(process.argv[1],'utf8');const m=s.match(/<script>([\s\S]*?)<\/script>/);if(!m)throw new Error('no inline script');fs.writeFileSync(process.argv[2],m[1].trim()+'\n')" D:/Repositories/Personal/ghr-web-scaffold/web/index.html web/public/theme-init.js
head -3 web/public/theme-init.js
```
Expected: the first line starts with `;(function () {` (or the scaffold's leading comment) and the file reads `localStorage.getItem("mode")`.

`web/index.html`:
```html
<!doctype html>
<html lang="en">
  <head>
    <meta charset="UTF-8" />
    <link rel="icon" type="image/svg+xml" href="/logo.svg" />
    <meta name="viewport" content="width=device-width, initial-scale=1.0" />
    <title>ghr</title>
    <script src="/theme-init.js"></script>
  </head>
  <body>
    <div id="root"></div>
    <script type="module" src="/src/main.tsx"></script>
  </body>
</html>
```

`web/public/logo.svg`:
```svg
<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 32 32"><rect width="32" height="32" rx="6" fill="#2563eb"/><text x="16" y="21" text-anchor="middle" font-family="Inter, sans-serif" font-size="12" font-weight="700" fill="#ffffff">ghr</text></svg>
```

`web/src/app.tsx`:
```tsx
import { ThemeProvider } from "darkraise-ui/theme"
import { themeConfig } from "./theme.config"

export function App() {
  return (
    <ThemeProvider config={themeConfig}>
      <main className="flex min-h-screen items-center justify-center">
        <h1 className="text-2xl font-medium">ghr</h1>
      </main>
    </ThemeProvider>
  )
}
```

`web/src/main.tsx`:
```tsx
import "@fontsource/inter/400.css"
import "@fontsource/inter/500.css"
import "@fontsource/inter/600.css"
import "@fontsource/inter/700.css"
import "@fontsource/jetbrains-mono/400.css"
import "@fontsource/jetbrains-mono/500.css"
import "./styles/globals.css"
import { StrictMode } from "react"
import { createRoot } from "react-dom/client"
import { App } from "./app"

const root = document.getElementById("root")
if (!root) throw new Error("index.html has no #root element")
createRoot(root).render(
  <StrictMode>
    <App />
  </StrictMode>,
)
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `timeout 300 npm --prefix web test`
Expected: PASS (4 tests).

- [ ] **Step 5: Build, then check the output and the embed**

```bash
timeout 300 npm --prefix web run lint
timeout 300 npm --prefix web run typecheck
timeout 600 npm --prefix web run build
test -f web/dist/index.html && test -f web/dist/theme-init.js && test -f web/dist/.gitkeep && echo dist-ok
grep -c "<script>" web/dist/index.html
git status --porcelain web/dist
rm -rf D:/Repositories/Personal/ghr-web-scaffold
```
Expected: `dist-ok`; `0` inline scripts; `git status` prints nothing for `web/dist`; the scaffold directory is gone.

- [ ] **Step 6: Commit**

```bash
git add web/index.html web/public web/src/main.tsx web/src/app.tsx web/src/app.test.tsx
git commit -m "feat(web): add the CSP-safe page and app skeleton"
```

---

### Task 5: CI: lint, test and build the web UI

**Files:**
- Modify: `.github/workflows/ci.yml`

**Interfaces:**
- Consumes: C2 scripts and `web/.nvmrc` (Tasks 1-4).
- Produces: nothing other tasks read.

**Items:** 2

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 0 - spec 0 - coupling 1 - risk 2 = 3

- [ ] **Step 1: Add the Node steps to the `test` job, after `go test`**

In `.github/workflows/ci.yml`, directly after the line `      - run: go test -race -count=1 -timeout 180s ./...`, insert:
```yaml
      - uses: actions/setup-node@v4
        with:
          node-version-file: web/.nvmrc
          cache: npm
          cache-dependency-path: web/package-lock.json
      - run: npm ci
        working-directory: web
      - run: npm run lint
        working-directory: web
      - run: npm run typecheck
        working-directory: web
      - run: npm test
        working-directory: web
```

- [ ] **Step 2: Build the UI in the `release` job before `go build`**

Directly after the `version` step's `run: |` block (the block ending with `echo "VERSION=$next" >> "$GITHUB_ENV"`) and before `      - name: build`, insert:
```yaml
      - uses: actions/setup-node@v4
        if: env.VERSION != ''
        with:
          node-version-file: web/.nvmrc
          cache: npm
          cache-dependency-path: web/package-lock.json
      - name: web
        if: env.VERSION != ''
        working-directory: web
        run: npm ci && npm run build && test -f dist/index.html
```

- [ ] **Step 3: Lint the workflow**

Run: `timeout 600 go run github.com/rhysd/actionlint/cmd/actionlint@v1.7.7 .github/workflows/ci.yml`
Expected: no output, exit 0.

- [ ] **Step 4: Replay the CI web steps locally**

```bash
cd web && timeout 900 npm ci && timeout 300 npm run lint && timeout 300 npm run typecheck && timeout 300 npm test && timeout 600 npm run build && test -f dist/index.html && echo ci-steps-ok; cd ..
git status --porcelain
```
Expected: `ci-steps-ok`; only `.github/workflows/ci.yml` is modified.

- [ ] **Step 5: Commit**

```bash
git add .github/workflows/ci.yml
git commit -m "ci: lint, test and build the web UI"
```

---

### Task 6: Go: web type fixtures

**Files:**
- Create: `web/fixtures_test.go`
- Create (generated): `web/src/api/fixtures/*.json` (14 files, C3)

**Interfaces:**
- Consumes: `internal/model`, `internal/config` (existing).
- Produces: C3.

**Items:** 3

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 0 = 3

- [ ] **Step 1: Write the test `web/fixtures_test.go`**

```go
package web

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/config"
	"github.com/darkraise/ghr/internal/model"
)

var fixtureTime = time.Date(2026, 10, 3, 14, 5, 0, 0, time.UTC)

func at(d time.Duration) time.Time { return fixtureTime.Add(d) }

func ptr[T any](v T) *T { return &v }

// TestTypeFixtures keeps web/src/api/fixtures in step with the Go types the
// web UI reads; the frontend's tests parse each fixture into its TypeScript
// type.
func TestTypeFixtures(t *testing.T) {
	update := os.Getenv("GHR_UPDATE_FIXTURES") == "1"
	for name, v := range fixtureValues() {
		path := filepath.Join("src", "api", "fixtures", name+".json")
		want, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		want = append(want, '\n')
		if update {
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, want, 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		got, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(bytes.ReplaceAll(got, []byte("\r\n"), []byte("\n")), want) {
			t.Errorf("web/%s is out of date with the Go types; regenerate with: GHR_UPDATE_FIXTURES=1 go test ./web/", filepath.ToSlash(path))
		}
	}
}

func fixtureValues() map[string]any {
	lastJob := model.HistoryEntry{ID: "h1", Repo: "darkmem", RunID: 101, RunNumber: "41", Workflow: "ci", JobName: "build",
		Conclusion: "success", StartedAt: at(-30 * time.Minute), FinishedAt: at(-25 * time.Minute),
		HTMLURL: "https://github.com/darkraise/darkmem/actions/runs/101/job/1"}
	return map[string]any{
		"status": model.Status{
			Now: fixtureTime, Epoch: "lz3k9a", Mode: "queue", GlobalMax: 2, RateRemaining: 4980, DiskPct: 61,
			Repos: []model.RepoStatus{
				{Name: "darkmem", Max: 2, Active: 1, Queued: 3, LastJob: &lastJob},
				{Name: "darkcloud", Paused: true, Max: 1},
				{Name: "old-repo", Paused: true, Removing: true, Max: 1, Error: "GitHub: not found"},
			},
			Instances: []model.InstanceStatus{
				{ID: "aaaaaa", Repo: "darkmem", RunnerName: "ghr-aaaaaa", State: "busy", Since: at(-5 * time.Minute),
					Job: &model.JobInfo{RunID: 102, RunNumber: "42", Workflow: "ci", Name: "test",
						HTMLURL: "https://github.com/darkraise/darkmem/actions/runs/102/job/2", StartedAt: at(-4 * time.Minute)}},
				{ID: "bbbbbb", Repo: "darkmem", RunnerName: "ghr-bbbbbb", State: "idle", Since: at(-time.Minute)},
			},
			Maintenance: model.MaintenanceStatus{LastStarted: ptr(at(-2 * time.Hour)), LastFinished: ptr(at(-2*time.Hour + time.Minute)),
				LastOutcome: "ok"},
			RunnerUpdate: model.RunnerUpdate{Installed: "2.337.0", Latest: "2.338.0", LatestPublished: ptr(at(-48 * time.Hour)),
				Deadline: ptr(at(20 * 24 * time.Hour)), CheckedAt: ptr(at(-time.Hour)), Queued: true,
				QueuedAt: ptr(at(-10 * time.Minute)), LastOutcome: "current", LastFinished: ptr(at(-24 * time.Hour))},
		},
		"events": []model.Event{
			{Seq: 1, Time: at(-3 * time.Minute), Level: "info", Msg: "ghr daemon started (owner darkraise, mode queue)"},
			{Seq: 2, Time: at(-2 * time.Minute), Level: "ok", Repo: "darkmem", Msg: "runner aaaaaa started"},
			{Seq: 3, Time: at(-time.Minute), Level: "warn", Repo: "darkcloud", Msg: "repo paused"},
		},
		"history": []model.HistoryEntry{lastJob,
			{ID: "h2", Repo: "darkcloud", RunID: 90, RunNumber: "7", Workflow: "deploy", JobName: "deploy", Conclusion: "failure",
				StartedAt: at(-3 * time.Hour), FinishedAt: at(-3*time.Hour + 90*time.Second)},
		},
		"log": model.LogChunk{
			Data: "[2026-10-03 14:01:00Z INFO Runner] Listening for Jobs\n[2026-10-03 14:01:05Z INFO Worker] Running job: test\n",
			Next: "Worker_1.log:96;Runner_1.log:52",
		},
		"steps": []model.Step{
			{Number: 1, Name: "Set up job", Status: "completed", Conclusion: "success"},
			{Number: 2, Name: "Run tests", Status: "in_progress"},
			{Number: 3, Name: "Post checkout", Status: "queued"},
		},
		"containers": []model.Container{{ID: "c0ffee12", Name: "ghr-aaaaaa-db-1", Image: "postgres:17", State: "running", Project: "ghr-aaaaaa"}},
		"metrics": model.Metrics{
			Samples: []model.MetricSample{
				{At: at(-3 * time.Minute), Live: 1, Queued: 2, CPU: ptr(12.5), Mem: ptr(int64(2 << 30))},
				{At: at(-2 * time.Minute), Live: 2, Queued: 1, CPU: ptr(48.0), Mem: ptr(int64(3 << 30))},
				{At: at(-time.Minute), Live: 1, Queued: 3},
			},
			CPU: ptr(31.0), MemUsed: ptr(int64(3 << 30)), MemTotal: ptr(int64(16 << 30)), DiskPct: 61,
		},
		"config": config.Config{Owner: "darkraise", Mode: "queue", GlobalMax: 2,
			PollInterval: config.Duration(10 * time.Second), StartTimeout: config.Duration(2 * time.Minute),
			IdleTimeout: config.Duration(5 * time.Minute), DiskHighWater: 80, BuildCacheKeep: "20GB",
			HistoryRetention: config.Duration(30 * 24 * time.Hour), Labels: []string{"homelab"},
			RunnerLimits: config.RunnerLimits{MemoryMax: "6G", CPUQuota: "200%"},
			Repos: []config.Repo{
				{Name: "darkmem", Max: ptr(2), Warm: ptr(1), Labels: []string{"gpu"}, CleanupNamePrefixes: []string{"darkmem-"}},
				{Name: "darkcloud", Paused: true},
			},
			Web: config.Web{Listen: "0.0.0.0:8080", Hosts: []string{"ghr.lan"}},
		},
		"storage": model.Storage{
			Toolchains: []model.Toolchain{{Tool: "node", Version: "22.11.0", Arch: "x64",
				Path: "/var/lib/ghr/toolcache/node/22.11.0/x64", Bytes: 190_000_000, InstalledAt: at(-72 * time.Hour)}},
			OtherToolCache: []model.Folder{{Name: "PyPy", Bytes: 80_000_000}},
			PackageCaches: []model.PackageCache{
				{Name: "nuget", Label: "NuGet", Paths: []string{".nuget/packages"}, Present: true, Bytes: 3_600_000_000,
					Files: 41000, LastWritten: ptr(at(-time.Hour))},
				{Name: "cargo", Label: "Cargo", Paths: []string{".cargo/registry", ".cargo/git"}},
			},
			Docker: model.DockerDisk{
				Rows: []model.DockerRow{
					{Type: "Images", Count: 10, Active: 2, Bytes: 8_100_000_000, Reclaimable: 7_100_000_000},
					{Type: "Build Cache", Count: 12, Bytes: 420_000_000, Reclaimable: 380_000_000},
				},
				BuildCacheTypes: []model.BuildCacheType{{Type: "regular", Count: 10, Bytes: 334_000_000, Reclaimable: 300_000_000}},
				DiskPct:         61,
			},
			MeasuredAt: ptr(at(-2 * time.Minute)),
			Operations: model.Operations{
				Current: &model.Operation{ID: "op3", Kind: "install", Target: "node 24", StartedAt: at(-30 * time.Second), Progress: "extracting"},
				Queued:  1,
				Recent: []model.Operation{{ID: "op2", Kind: "clear", Target: "nuget", StartedAt: at(-time.Hour),
					FinishedAt: ptr(at(-time.Hour + time.Minute)), Outcome: "ok", Message: "cleared NuGet (3.6 GB freed)"}},
			},
			LastPrune: &model.LastPrune{Trigger: "manual", Scope: "build-cache-all", StartedAt: at(-50 * time.Minute),
				FinishedAt: ptr(at(-49 * time.Minute)), Outcome: "ok", Steps: []model.PruneStep{{Name: "all build cache", Freed: 6_800_000}}},
		},
		"token": model.TokenStatus{State: "ok", CheckedAt: ptr(at(-time.Minute)), RateRemaining: ptr(4980), RateLimit: ptr(5000),
			RateReset: ptr(at(40 * time.Minute)), ExpiresAt: ptr(at(60 * 24 * time.Hour))},
		"label-check": model.LabelCheck{State: "done", CheckedAt: ptr(at(-time.Minute)),
			Groups: []model.LabelGroup{{Labels: []string{"homelab", "self-hosted"}, Jobs: []string{"ci / build", "ci / test"},
				Count: 12, LastSeen: at(-time.Hour)}}},
		"registrations": []model.Registration{{ID: 5, Name: "ghr-aaaaaa", Status: "online", Busy: true,
			Labels: []string{"self-hosted", "homelab"}, GHR: true}},
		"available-repos": []model.AvailableRepo{{Name: "darkmem", Private: true, Configured: true}, {Name: "new-repo", Private: true}},
		"toolchain-choices": []model.ToolchainChoice{{Spec: "22.11.0", Version: "22.11.0", LTS: true}, {Spec: "24.9.0", Version: "24.9.0"}},
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `timeout 300 go test ./web/ -run TestTypeFixtures`
Expected: FAIL — 14 lines `web/src/api/fixtures/<name>.json is out of date with the Go types; regenerate with: GHR_UPDATE_FIXTURES=1 go test ./web/`.

- [ ] **Step 3: Generate the fixtures**

```bash
timeout 60 gofmt -w web/fixtures_test.go
GHR_UPDATE_FIXTURES=1 timeout 300 go test ./web/ -run TestTypeFixtures
ls web/src/api/fixtures | wc -l
grep -c '"last_prune"' web/src/api/fixtures/storage.json
```
Expected: `14` files; `1`.

- [ ] **Step 4: Run the test to verify it passes, then the Go checks**

```bash
timeout 300 go test ./web/ -run TestTypeFixtures -v 2>&1 | tail -2
timeout 120 gofmt -l internal cmd web/*.go
timeout 300 go vet ./...
```
Expected: `--- PASS: TestTypeFixtures` and `ok`; gofmt prints nothing; vet exits 0.

- [ ] **Step 5: Commit**

```bash
git add web/fixtures_test.go web/src/api/fixtures
git commit -m "test(web): generate type fixtures from Go"
```

---

### Task 7: web: API types

**Files:**
- Create: `web/src/api/types.ts`
- Test: `web/src/api/types.test.ts`, `web/src/test/fixtures.ts`

**Interfaces:**
- Consumes: C3.
- Produces: C4.

**Items:** 3

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 0 = 3

- [ ] **Step 1: Write the failing test `web/src/api/types.test.ts`**

```ts
import { describe, expect, it } from "vitest"
import availableReposJson from "./fixtures/available-repos.json"
import choicesJson from "./fixtures/toolchain-choices.json"
import configJson from "./fixtures/config.json"
import containersJson from "./fixtures/containers.json"
import eventsJson from "./fixtures/events.json"
import historyJson from "./fixtures/history.json"
import labelCheckJson from "./fixtures/label-check.json"
import logJson from "./fixtures/log.json"
import metricsJson from "./fixtures/metrics.json"
import registrationsJson from "./fixtures/registrations.json"
import statusJson from "./fixtures/status.json"
import stepsJson from "./fixtures/steps.json"
import storageJson from "./fixtures/storage.json"
import tokenJson from "./fixtures/token.json"
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
} from "./types"

// Assigning each fixture to its type makes `tsc --noEmit` fail when a Go
// field the UI declares is renamed or retyped.
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
const choices: ToolchainChoice[] = choicesJson

describe("type fixtures", () => {
  it("status carries the fields the UI reads", () => {
    expect(status.epoch).toBe("lz3k9a")
    expect(status.instances[0]?.job?.run_number).toBe("42")
    expect(status.instances[0]?.job?.html_url).toContain("https://")
    expect(status.repos[0]?.last_job?.finished_at).toBeTruthy()
    expect(status.runner_update.deadline).toBeTruthy()
    expect(status.maintenance.last_outcome).toBe("ok")
  })

  it("the live feeds carry their cursors", () => {
    expect(events.map((e) => e.seq)).toEqual([1, 2, 3])
    expect(log.next).not.toBe("")
    expect(history[0]?.conclusion).toBe("success")
    expect(steps[1]?.status).toBe("in_progress")
    expect(containers[0]?.project).toBe("ghr-aaaaaa")
    expect(metrics.samples).toHaveLength(3)
  })

  it("config durations arrive as strings", () => {
    expect(config.poll_interval).toBe("10s")
    expect(config.history_retention).toBe("30d")
    expect(config.repos?.[0]?.max).toBe(2)
  })

  it("the later pages' types parse too", () => {
    expect(storage.operations.current?.progress).toBe("extracting")
    expect(storage.last_prune?.steps?.[0]?.freed).toBe(6800000)
    expect(token.rate_limit).toBe(5000)
    expect(labelCheck.groups[0]?.labels).toContain("homelab")
    expect(registrations[0]?.ghr).toBe(true)
    expect(availableRepos[1]?.configured).toBe(false)
    expect(choices[0]?.lts).toBe(true)
  })
})
```

- [ ] **Step 2: Run it to verify it fails**

Run: `timeout 300 npm --prefix web test -- src/api/types.test.ts`
Expected: FAIL — `Failed to resolve import "./types"` (the type-only import is erased, so Vitest may instead pass; in that case confirm the failure with `timeout 300 npm --prefix web run typecheck`, which reports `Cannot find module './types'`).

- [ ] **Step 3: Write `web/src/api/types.ts`**

```ts
export interface AuthState {
  setup_required: boolean
  authenticated: boolean
}

export interface JobInfo {
  run_id: number
  run_number: string
  workflow: string
  name: string
  html_url?: string
  started_at: string
}

export interface InstanceStatus {
  id: string
  repo: string
  runner_name: string
  state: string
  since: string
  job?: JobInfo
}

export interface HistoryEntry {
  id: string
  repo: string
  run_id: number
  run_number: string
  workflow: string
  job_name: string
  conclusion: string
  started_at: string
  finished_at: string
  html_url?: string
}

export interface RepoStatus {
  name: string
  paused: boolean
  removing?: boolean
  max: number
  active: number
  queued: number
  error?: string
  last_job?: HistoryEntry
}

export interface MaintenanceStatus {
  running: boolean
  last_started?: string
  last_finished?: string
  last_outcome?: string
}

export interface RunnerUpdate {
  installed?: string
  latest?: string
  latest_published?: string
  deadline?: string
  checked_at?: string
  check_error?: string
  queued?: boolean
  queued_at?: string
  running?: boolean
  last_outcome?: string
  last_error?: string
  last_finished?: string
}

export interface Status {
  now: string
  epoch: string
  mode: string
  global_max: number
  degraded: boolean
  degraded_reason?: string
  rate_remaining: number
  disk_pct: number
  repos: RepoStatus[]
  instances: InstanceStatus[]
  maintenance: MaintenanceStatus
  runner_update: RunnerUpdate
}

export interface GhrEvent {
  seq: number
  time: string
  level: string
  repo?: string
  msg: string
}

export interface Step {
  number: number
  name: string
  status: string
  conclusion: string
}

export interface LogChunk {
  data: string
  next: string
}

export interface Container {
  id: string
  name: string
  image: string
  state: string
  project: string
}

export interface MetricSample {
  at: string
  live: number
  queued: number
  cpu?: number
  mem?: number
}

export interface Metrics {
  samples: MetricSample[]
  cpu?: number
  mem_used?: number
  mem_total?: number
  disk_pct: number
}

export interface RunnerLimits {
  memory_max: string
  cpu_quota: string
}

export interface WebConfig {
  listen?: string
  hosts?: string[]
}

export interface RepoConfig {
  name: string
  max?: number
  warm?: number
  labels?: string[]
  cleanup_name_prefixes?: string[]
  paused?: boolean
  removing?: boolean
}

export interface Config {
  owner: string
  mode: string
  global_max: number
  poll_interval: string
  start_timeout: string
  idle_timeout: string
  disk_high_water: number
  build_cache_keep: string
  history_retention: string
  labels: string[] | null
  runner_limits: RunnerLimits
  repos: RepoConfig[] | null
  web?: WebConfig
}

export interface TokenStatus {
  state: string
  checked_at?: string
  reason?: string
  rate_remaining?: number
  rate_limit?: number
  rate_reset?: string
  expires_at?: string
}

export interface Registration {
  id: number
  name: string
  status: string
  busy: boolean
  labels: string[]
  ghr: boolean
}

export interface LabelGroup {
  labels: string[]
  jobs: string[]
  more: number
  count: number
  last_seen: string
}

export interface LabelCheck {
  state: string
  checked_at?: string
  partial: boolean
  error?: string
  groups: LabelGroup[]
}

export interface AvailableRepo {
  name: string
  private: boolean
  configured: boolean
}

export interface Toolchain {
  tool: string
  version: string
  arch: string
  path: string
  bytes: number
  installed_at: string
}

export interface Folder {
  name: string
  bytes: number
}

export interface PackageCache {
  name: string
  label: string
  paths: string[] | null
  present: boolean
  bytes: number
  files: number
  last_written?: string
}

export interface DockerRow {
  type: string
  count: number
  active: number
  bytes: number
  reclaimable: number
}

export interface BuildCacheType {
  type: string
  count: number
  bytes: number
  reclaimable: number
}

export interface DockerDisk {
  rows: DockerRow[] | null
  build_cache_types: BuildCacheType[] | null
  disk_pct: number
}

export interface Operation {
  id: string
  kind: string
  target: string
  started_at: string
  progress?: string
  finished_at?: string
  outcome?: string
  message?: string
}

export interface Operations {
  current: Operation | null
  queued: number
  recent: Operation[] | null
}

export interface PruneStep {
  name: string
  freed: number
  error?: string
}

export interface LastPrune {
  trigger: string
  scope: string
  started_at: string
  finished_at?: string
  outcome?: string
  steps: PruneStep[] | null
}

export interface Storage {
  toolchains: Toolchain[] | null
  other_tool_cache: Folder[] | null
  package_caches: PackageCache[] | null
  docker: DockerDisk
  measured_at?: string
  measuring: boolean
  measure_error?: string
  operations: Operations
  last_prune: LastPrune | null
}

export interface ToolchainChoice {
  spec: string
  version: string
  lts?: boolean
}
```

- [ ] **Step 4: Write the shared test helper `web/src/test/fixtures.ts`**

```ts
import configJson from "@/api/fixtures/config.json"
import containersJson from "@/api/fixtures/containers.json"
import eventsJson from "@/api/fixtures/events.json"
import historyJson from "@/api/fixtures/history.json"
import logJson from "@/api/fixtures/log.json"
import metricsJson from "@/api/fixtures/metrics.json"
import statusJson from "@/api/fixtures/status.json"
import stepsJson from "@/api/fixtures/steps.json"
import type { Config, Container, GhrEvent, HistoryEntry, LogChunk, Metrics, Status, Step } from "@/api/types"

const status: Status = statusJson
const events: GhrEvent[] = eventsJson
const history: HistoryEntry[] = historyJson
const log: LogChunk = logJson
const steps: Step[] = stepsJson
const containers: Container[] = containersJson
const metrics: Metrics = metricsJson
const config: Config = configJson

export const fixtures = { status, events, history, log, steps, containers, metrics, config }

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

- [ ] **Step 5: Run the tests and the type check to verify they pass**

```bash
timeout 300 npm --prefix web test -- src/api/types.test.ts
timeout 300 npm --prefix web run typecheck
timeout 300 npm --prefix web run lint
```
Expected: PASS (4 tests); typecheck and lint exit 0.

- [ ] **Step 6: Commit**

```bash
git add web/src/api/types.ts web/src/api/types.test.ts web/src/test/fixtures.ts
git commit -m "feat(web): add the API types"
```

---

### Task 8: web: API client

**Files:**
- Create: `web/src/api/client.ts`
- Test: `web/src/api/client.test.ts`, `web/src/test/api.ts`

**Interfaces:**
- Consumes: C4 types.
- Produces: C5.

**Items:** 3

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 1 = 4

- [ ] **Step 1: Write the fetch mock `web/src/test/api.ts`**

```ts
import { vi } from "vitest"

export interface Call {
  method: string
  path: string
  search: string
  body: unknown
}

type Handler = (req: { url: URL; method: string; body: unknown }) => unknown

export function json(value: unknown, status = 200): Response {
  return new Response(JSON.stringify(value), { status, headers: { "Content-Type": "application/json" } })
}

export function noContent(): Response {
  return new Response(null, { status: 204 })
}

// Routes are keyed "<METHOD> <path>". A value is a JSON body, or a handler
// returning a JSON body or a Response; a Response body can be read once, so
// a route that answers with a Response must be a handler.
export function mockApi(routes: Record<string, unknown>) {
  const calls: Call[] = []
  const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit): Promise<Response> => {
    const raw = typeof input === "string" ? input : input instanceof URL ? input.href : input.url
    const url = new URL(raw, "http://localhost")
    const method = (init?.method ?? "GET").toUpperCase()
    const body = typeof init?.body === "string" ? (JSON.parse(init.body) as unknown) : undefined
    calls.push({ method, path: url.pathname, search: url.search, body })
    const route = routes[`${method} ${url.pathname}`]
    if (route === undefined) return json({ error: `no mock for ${method} ${url.pathname}` }, 404)
    const out = typeof route === "function" ? (route as Handler)({ url, method, body }) : route
    return out instanceof Response ? out : json(out)
  })
  vi.stubGlobal("fetch", fetchMock)
  return { calls, fetchMock }
}
```

- [ ] **Step 2: Write the failing test `web/src/api/client.test.ts`**

```ts
import { afterEach, describe, expect, it, vi } from "vitest"
import { json, mockApi, noContent } from "@/test/api"
import { ApiError, api, setUnauthorizedHandler } from "./client"

afterEach(() => setUnauthorizedHandler(() => {}))

describe("api client", () => {
  it("sends X-GHR and same-origin credentials", async () => {
    const { fetchMock } = mockApi({ "GET /api/status": { epoch: "e" } })
    await api.status()
    const init = fetchMock.mock.calls[0]?.[1]
    expect(new Headers(init?.headers).get("X-GHR")).toBe("1")
    expect(init?.credentials).toBe("same-origin")
  })

  it("hands an /api 401 to the unauthorized handler", async () => {
    const onUnauthorized = vi.fn()
    setUnauthorizedHandler(onUnauthorized)
    mockApi({ "GET /api/status": () => json({ error: "not logged in" }, 401) })
    await expect(api.status()).rejects.toMatchObject({ status: 401, message: "not logged in" })
    expect(onUnauthorized).toHaveBeenCalledOnce()
  })

  it("throws a wrong-password 401 from /auth without redirecting", async () => {
    const onUnauthorized = vi.fn()
    setUnauthorizedHandler(onUnauthorized)
    mockApi({
      "POST /auth/login": () => json({ error: "wrong password" }, 401),
      "POST /auth/password": () => json({ error: "wrong password" }, 401),
    })
    await expect(api.login("x")).rejects.toBeInstanceOf(ApiError)
    await expect(api.changePassword("old password!", "new password!")).rejects.toMatchObject({
      status: 401,
      message: "wrong password",
    })
    expect(onUnauthorized).not.toHaveBeenCalled()
  })

  it("parses retry_at into ApiError.retryAt", async () => {
    mockApi({
      "POST /auth/login": () =>
        json({ error: "too many failed logins; retry after 2026-10-06T14:20:00Z", retry_at: "2026-10-06T14:20:00Z" }, 429),
    })
    const err = await api.login("x").catch((e: unknown) => e)
    expect(err).toBeInstanceOf(ApiError)
    expect((err as ApiError).status).toBe(429)
    expect((err as ApiError).retryAt?.toISOString()).toBe("2026-10-06T14:20:00.000Z")
  })

  it("keeps a plain-text error body as the message", async () => {
    mockApi({ "GET /api/status": () => new Response("404 page not found\n", { status: 404 }) })
    await expect(api.status()).rejects.toMatchObject({ status: 404, message: "404 page not found" })
  })

  it("sends JSON bodies and builds query strings", async () => {
    const { calls } = mockApi({ "POST /auth/setup": () => noContent(), "GET /api/history": [] })
    await api.setup("correct horse battery")
    await api.history("darkmem", "", 200)
    expect(calls[0]?.body).toEqual({ password: "correct horse battery" })
    expect(calls[1]?.search).toBe("?repo=darkmem&conclusion=&limit=200")
  })

  it("escapes runner IDs and cursors", async () => {
    const { calls } = mockApi({ "GET /api/runners/a%2Fb/log": { data: "", next: "" } })
    await api.log("a/b", "x;y")
    expect(calls[0]?.search).toBe("?cursor=x%3By")
  })
})
```

- [ ] **Step 3: Run it to verify it fails**

Run: `timeout 300 npm --prefix web test -- src/api/client.test.ts`
Expected: FAIL — `Failed to resolve import "./client"`.

- [ ] **Step 4: Write `web/src/api/client.ts`**

```ts
import type { AuthState, Config, Container, GhrEvent, HistoryEntry, LogChunk, Metrics, Status, Step } from "./types"

export class ApiError extends Error {
  readonly status: number
  readonly retryAt?: Date

  constructor(status: number, message: string, retryAt?: Date) {
    super(message)
    this.name = "ApiError"
    this.status = status
    this.retryAt = retryAt
  }
}

let onUnauthorized: () => void = () => {}

export function setUnauthorizedHandler(fn: () => void): void {
  onUnauthorized = fn
}

export async function request<T>(method: string, path: string, body?: unknown, signal?: AbortSignal): Promise<T> {
  const headers: Record<string, string> = { "X-GHR": "1" }
  if (body !== undefined) headers["Content-Type"] = "application/json"
  const res = await fetch(path, {
    method,
    headers,
    credentials: "same-origin",
    body: body === undefined ? undefined : JSON.stringify(body),
    signal,
  })
  if (res.ok) {
    if (res.status === 202 || res.status === 204) return undefined as T
    return (await res.json()) as T
  }
  const err = await toError(res)
  // Only an expired session on the API means "log in again"; a 401 from
  // /auth/login or /auth/password is a wrong password the form shows.
  if (res.status === 401 && path.startsWith("/api/")) onUnauthorized()
  throw err
}

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

const query = (params: Record<string, string | number>) =>
  new URLSearchParams(Object.entries(params).map(([k, v]) => [k, String(v)])).toString()
const seg = encodeURIComponent

export const api = {
  authState: (signal?: AbortSignal) => request<AuthState>("GET", "/auth/state", undefined, signal),
  setup: (password: string) => request<void>("POST", "/auth/setup", { password }),
  login: (password: string) => request<void>("POST", "/auth/login", { password }),
  logout: () => request<void>("POST", "/auth/logout"),
  changePassword: (current: string, next: string) => request<void>("POST", "/auth/password", { current, new: next }),
  status: (signal?: AbortSignal) => request<Status>("GET", "/api/status", undefined, signal),
  events: (after: number, signal?: AbortSignal) =>
    request<GhrEvent[]>("GET", `/api/events?${query({ after })}`, undefined, signal),
  history: (repo: string, conclusion: string, limit: number, signal?: AbortSignal) =>
    request<HistoryEntry[]>("GET", `/api/history?${query({ repo, conclusion, limit })}`, undefined, signal),
  log: (id: string, cursor: string, signal?: AbortSignal) =>
    request<LogChunk>("GET", `/api/runners/${seg(id)}/log?${query({ cursor })}`, undefined, signal),
  steps: (id: string, signal?: AbortSignal) => request<Step[]>("GET", `/api/runners/${seg(id)}/steps`, undefined, signal),
  containers: (id: string, signal?: AbortSignal) =>
    request<Container[]>("GET", `/api/runners/${seg(id)}/containers`, undefined, signal),
  metrics: (signal?: AbortSignal) => request<Metrics>("GET", "/api/metrics", undefined, signal),
  config: (signal?: AbortSignal) => request<Config>("GET", "/api/config", undefined, signal),
  stopRunner: (id: string) => request<void>("DELETE", `/api/runners/${seg(id)}`),
  pauseAll: () => request<void>("POST", "/api/pause-all"),
  resumeAll: () => request<void>("POST", "/api/resume-all"),
}
```

- [ ] **Step 5: Run the tests to verify they pass**

```bash
timeout 300 npm --prefix web test -- src/api/client.test.ts
timeout 300 npm --prefix web run typecheck
timeout 300 npm --prefix web run lint
```
Expected: PASS (7 tests); typecheck and lint exit 0.

- [ ] **Step 6: Commit**

```bash
git add web/src/api/client.ts web/src/api/client.test.ts web/src/test/api.ts
git commit -m "feat(web): add the API client"
```

---

### Task 9: web: polling hooks

**Files:**
- Create: `web/src/api/hooks.ts`
- Test: `web/src/api/hooks.test.tsx`, `web/src/test/query.tsx`

**Interfaces:**
- Consumes: C4 types, C5 `api` and `mockApi`.
- Produces: C6.

**Items:** 4

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 1 = 4

- [ ] **Step 1: Write the test wrapper `web/src/test/query.tsx`**

```tsx
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import type { ReactNode } from "react"

export function withQuery() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  )
  return { queryClient, wrapper }
}
```

- [ ] **Step 2: Write the failing test `web/src/api/hooks.test.tsx`**

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
  it("polls by cursor and starts over when the epoch changes", async () => {
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
  })

  it("waits for an epoch before polling", async () => {
    const { calls } = mockApi({ "GET /api/events": [] })
    const { wrapper } = withQuery()
    renderHook(() => useEvents(undefined), { wrapper })
    await new Promise((r) => setTimeout(r, 1200))
    expect(calls).toHaveLength(0)
  })
})

describe("useLogTail", () => {
  it("advances the cursor on every poll", async () => {
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
  })

  it("does not poll while the log is closed", async () => {
    const { calls } = mockApi({ "GET /api/runners/aaaaaa/log": { data: "x", next: "c1" } })
    const { wrapper } = withQuery()
    renderHook(() => useLogTail("aaaaaa", false), { wrapper })
    await new Promise((r) => setTimeout(r, 1200))
    expect(calls).toHaveLength(0)
  })
})
```

- [ ] **Step 3: Run it to verify it fails**

Run: `timeout 300 npm --prefix web test -- src/api/hooks.test.tsx`
Expected: FAIL — `Failed to resolve import "./hooks"`.

- [ ] **Step 4: Write `web/src/api/hooks.ts`**

```ts
import { useQuery, useQueryClient } from "@tanstack/react-query"
import { api } from "./client"
import type { GhrEvent, LogChunk } from "./types"

export const POLL_FAST = 1000
export const POLL_SLOW = 5000
export const MAX_EVENTS = 200
export const MAX_LOG = 256 * 1024

export const keys = {
  auth: ["auth"] as const,
  status: ["status"] as const,
  config: ["config"] as const,
  metrics: ["metrics"] as const,
  events: (epoch: string) => ["events", epoch] as const,
  history: (repo: string, conclusion: string) => ["history", repo, conclusion] as const,
  steps: (id: string) => ["steps", id] as const,
  containers: (id: string) => ["containers", id] as const,
  log: (id: string) => ["log", id] as const,
}

export function useStatus() {
  return useQuery({ queryKey: keys.status, queryFn: ({ signal }) => api.status(signal), refetchInterval: POLL_FAST })
}

export function useConfig() {
  return useQuery({ queryKey: keys.config, queryFn: ({ signal }) => api.config(signal), refetchInterval: POLL_SLOW })
}

export function useMetrics() {
  return useQuery({ queryKey: keys.metrics, queryFn: ({ signal }) => api.metrics(signal), refetchInterval: POLL_SLOW })
}

export function useHistory(repo: string, conclusion: string) {
  return useQuery({
    queryKey: keys.history(repo, conclusion),
    queryFn: ({ signal }) => api.history(repo, conclusion, 200, signal),
    refetchInterval: POLL_SLOW,
  })
}

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

export function appendEvents(prev: GhrEvent[], next: GhrEvent[]): GhrEvent[] {
  const last = prev.at(-1)?.seq ?? 0
  const fresh = next.filter((e) => e.seq > last)
  if (fresh.length === 0) return prev
  return [...prev, ...fresh].slice(-MAX_EVENTS)
}

// The epoch is part of the key: a daemon restart restarts the sequence
// numbers, so a new epoch starts an empty list with the cursor at 0.
export function useEvents(epoch: string | undefined): GhrEvent[] {
  const queryClient = useQueryClient()
  const key = keys.events(epoch ?? "")
  const q = useQuery({
    queryKey: key,
    queryFn: async ({ signal }) => {
      const prev = queryClient.getQueryData<GhrEvent[]>(key) ?? []
      return appendEvents(prev, await api.events(prev.at(-1)?.seq ?? 0, signal))
    },
    enabled: epoch !== undefined,
    refetchInterval: POLL_FAST,
  })
  return q.data ?? []
}

export interface LogTail {
  text: string
  next: string
}

export function appendLog(prev: LogTail, chunk: LogChunk): LogTail {
  const text = prev.text + chunk.data
  return { text: text.length > MAX_LOG ? text.slice(-MAX_LOG) : text, next: chunk.next }
}

export function useLogTail(id: string, enabled: boolean) {
  const queryClient = useQueryClient()
  const key = keys.log(id)
  return useQuery({
    queryKey: key,
    queryFn: async ({ signal }) => {
      const prev = queryClient.getQueryData<LogTail>(key) ?? { text: "", next: "" }
      return appendLog(prev, await api.log(id, prev.next, signal))
    },
    enabled,
    refetchInterval: enabled ? POLL_FAST : false,
  })
}
```

- [ ] **Step 5: Run the tests to verify they pass**

```bash
timeout 300 npm --prefix web test -- src/api/hooks.test.tsx
timeout 300 npm --prefix web run typecheck
timeout 300 npm --prefix web run lint
```
Expected: PASS (7 tests); typecheck and lint exit 0.

- [ ] **Step 6: Commit**

```bash
git add web/src/api/hooks.ts web/src/api/hooks.test.tsx web/src/test/query.tsx
git commit -m "feat(web): add the polling hooks"
```

---

### Task 10: web: formatting helpers

**Files:**
- Create: `web/src/lib/format.ts`, `web/src/lib/use-now.ts`
- Test: `web/src/lib/format.test.ts`

**Interfaces:**
- Consumes: C4 types.
- Produces: C7.

**Items:** 7, 8, 9, 10

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 0 = 3

- [ ] **Step 1: Write the failing test `web/src/lib/format.test.ts`**

```ts
import { act, renderHook } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"
import type { InstanceStatus, MetricSample } from "@/api/types"
import { ago, clock, dateTime, dateTimeSec, dur, elapsed, fmtMem, hhmm, humanBytes, isZeroTime, maxText, series } from "./format"
import { useNow } from "./use-now"

afterEach(() => vi.useRealTimers())

describe("dur", () => {
  it("prints minutes and seconds under an hour", () => {
    expect(dur(65_000)).toBe("1m05s")
    expect(dur(0)).toBe("0m00s")
    expect(dur(-5_000)).toBe("0m00s")
  })
  it("prints hours and minutes from an hour", () => expect(dur(3_725_000)).toBe("1h02m"))
  it("rounds to the second", () => expect(dur(59_600)).toBe("1m00s"))
})

describe("ago", () => {
  it("matches the TUI's buckets", () => {
    expect(ago(30_000)).toBe("just now")
    expect(ago(5 * 60_000)).toBe("5m ago")
    expect(ago(3 * 3_600_000)).toBe("3h ago")
    expect(ago(47 * 3_600_000)).toBe("47h ago")
    expect(ago(48 * 3_600_000)).toBe("2d ago")
  })
})

describe("humanBytes", () => {
  it("uses decimal units with one decimal", () => {
    expect(humanBytes(0)).toBe("0 B")
    expect(humanBytes(999)).toBe("999 B")
    expect(humanBytes(1000)).toBe("1.0 kB")
    expect(humanBytes(1_234_567)).toBe("1.2 MB")
    expect(humanBytes(6_571_000_000)).toBe("6.6 GB")
  })
  it("rounds before picking the unit", () => {
    expect(humanBytes(999_949)).toBe("999.9 kB")
    expect(humanBytes(999_950)).toBe("1.0 MB")
    expect(humanBytes(999_950_000)).toBe("1.0 GB")
  })
})

describe("small formatters", () => {
  it("fmtMem prints binary G or M", () => {
    expect(fmtMem(3 * 2 ** 30)).toBe("3.0G")
    expect(fmtMem(512 * 2 ** 20)).toBe("512M")
  })
  it("maxText shows 0 as unlimited", () => {
    expect(maxText(0)).toBe("∞")
    expect(maxText(3)).toBe("3")
  })
  it("isZeroTime spots Go's zero time", () => {
    expect(isZeroTime("0001-01-01T00:00:00Z")).toBe(true)
    expect(isZeroTime(undefined)).toBe(true)
    expect(isZeroTime("2026-10-03T14:05:00Z")).toBe(false)
  })
  it("formats local times (TZ=UTC in tests)", () => {
    expect(clock("2026-10-03T14:05:09Z")).toBe("14:05:09")
    expect(hhmm(new Date("2026-10-06T14:20:00Z"))).toBe("14:20")
    expect(dateTime("2026-10-03T14:05:09Z")).toBe("2026-10-03 14:05")
    expect(dateTimeSec("2026-10-03T14:05:09Z")).toBe("2026-10-03 14:05:09")
  })
})

describe("elapsed", () => {
  const now = Date.parse("2026-10-03T14:05:00Z")
  const base: InstanceStatus = { id: "a", repo: "r", runner_name: "n", state: "busy", since: "2026-10-03T14:00:00Z" }
  it("counts from the job's start when there is one", () => {
    const job = { run_id: 1, run_number: "1", workflow: "w", name: "j", started_at: "2026-10-03T14:04:00Z" }
    expect(elapsed({ ...base, job }, now)).toBe("1m00s")
  })
  it("counts from the state change when the job has not started", () => {
    const job = { run_id: 1, run_number: "1", workflow: "w", name: "j", started_at: "0001-01-01T00:00:00Z" }
    expect(elapsed({ ...base, job }, now)).toBe("5m00s")
    expect(elapsed(base, now)).toBe("5m00s")
  })
})

describe("series", () => {
  it("leaves a gap where samples are more than 90 s apart", () => {
    const s = (at: string, live: number): MetricSample => ({ at, live, queued: 0 })
    const samples = [s("2026-10-03T14:00:00Z", 1), s("2026-10-03T14:01:00Z", 2), s("2026-10-03T14:05:00Z", 3)]
    expect(series(samples, (x) => x.live)).toEqual([1, 2, null, 3])
    expect(series(samples, (x) => x.cpu)).toEqual([null, null, null, null])
  })
})

describe("useNow", () => {
  it("ticks every second", () => {
    vi.useFakeTimers({ now: new Date("2026-10-03T14:05:00Z") })
    const { result } = renderHook(() => useNow())
    expect(result.current).toBe(Date.parse("2026-10-03T14:05:00Z"))
    act(() => {
      vi.advanceTimersByTime(1000)
    })
    expect(result.current).toBe(Date.parse("2026-10-03T14:05:01Z"))
  })
})
```

- [ ] **Step 2: Run it to verify it fails**

Run: `timeout 300 npm --prefix web test -- src/lib/format.test.ts`
Expected: FAIL — `Failed to resolve import "./format"`.

- [ ] **Step 3: Write `web/src/lib/format.ts` and `web/src/lib/use-now.ts`**

`web/src/lib/format.ts`:
```ts
import type { InstanceStatus, MetricSample } from "@/api/types"

const pad = (n: number) => String(n).padStart(2, "0")

export function dur(ms: number): string {
  const total = Math.max(0, Math.round(ms / 1000))
  const h = Math.floor(total / 3600)
  const m = Math.floor(total / 60) % 60
  const s = total % 60
  return h > 0 ? `${h}h${pad(m)}m` : `${m}m${pad(s)}s`
}

export function ago(ms: number): string {
  const minutes = ms / 60_000
  if (minutes < 1) return "just now"
  if (minutes < 60) return `${Math.floor(minutes)}m ago`
  const hours = minutes / 60
  if (hours < 48) return `${Math.floor(hours)}h ago`
  return `${Math.floor(hours / 24)}d ago`
}

const UNITS = ["B", "kB", "MB", "GB", "TB", "PB"]

export function humanBytes(n: number): string {
  let f = n
  let i = 0
  while (f >= 1000 && i < UNITS.length - 1) {
    f /= 1000
    i++
  }
  if (i === 0) return `${n} B`
  let s = f.toFixed(1)
  if (s === "1000.0" && i < UNITS.length - 1) {
    f /= 1000
    i++
    s = f.toFixed(1)
  }
  return `${s} ${UNITS[i]}`
}

export function fmtMem(bytes: number): string {
  return bytes >= 2 ** 30 ? `${(bytes / 2 ** 30).toFixed(1)}G` : `${Math.floor(bytes / 2 ** 20)}M`
}

export function maxText(n: number): string {
  return n === 0 ? "∞" : String(n)
}

export function isZeroTime(iso: string | undefined): boolean {
  return !iso || iso.startsWith("0001-01-01")
}

export function hhmm(d: Date): string {
  return `${pad(d.getHours())}:${pad(d.getMinutes())}`
}

export function clock(iso: string): string {
  const d = new Date(iso)
  return `${hhmm(d)}:${pad(d.getSeconds())}`
}

export function dateTime(iso: string): string {
  const d = new Date(iso)
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${hhmm(d)}`
}

export function dateTimeSec(iso: string): string {
  return `${dateTime(iso)}:${pad(new Date(iso).getSeconds())}`
}

export function elapsed(instance: InstanceStatus, now: number): string {
  const job = instance.job
  const start = job && !isZeroTime(job.started_at) ? job.started_at : instance.since
  return dur(now - Date.parse(start))
}

export function series(samples: MetricSample[], pick: (s: MetricSample) => number | undefined): (number | null)[] {
  const out: (number | null)[] = []
  let prev: number | undefined
  for (const s of samples) {
    const t = Date.parse(s.at)
    if (prev !== undefined && t - prev > 90_000) out.push(null)
    prev = t
    out.push(pick(s) ?? null)
  }
  return out
}
```

`web/src/lib/use-now.ts`:
```ts
import { useEffect, useState } from "react"

export function useNow(): number {
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    const timer = setInterval(() => setNow(Date.now()), 1000)
    return () => clearInterval(timer)
  }, [])
  return now
}
```

- [ ] **Step 4: Run the tests to verify they pass**

```bash
timeout 300 npm --prefix web test -- src/lib/format.test.ts
timeout 300 npm --prefix web run typecheck
timeout 300 npm --prefix web run lint
```
Expected: PASS (14 tests); typecheck and lint exit 0.

- [ ] **Step 5: Commit**

```bash
git add web/src/lib/format.ts web/src/lib/use-now.ts web/src/lib/format.test.ts
git commit -m "feat(web): add formatting helpers"
```

---

### Task 11: web: state badge, sparkline and clipboard

**Files:**
- Create: `web/src/components/state-badge.tsx`, `web/src/components/sparkline.tsx`, `web/src/lib/clipboard.ts`
- Test: `web/src/components/components.test.tsx`

**Interfaces:**
- Consumes: nothing from earlier tasks beyond the toolchain.
- Produces: C8.

**Items:** 7, 8

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 0 = 3

- [ ] **Step 1: Write the failing test `web/src/components/components.test.tsx`**

```tsx
import { render, screen } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"
import { copyText } from "@/lib/clipboard"
import { Sparkline } from "./sparkline"
import { StateBadge, stateVariant } from "./state-badge"

function secure(value: boolean) {
  Object.defineProperty(window, "isSecureContext", { value, configurable: true })
}

afterEach(() => {
  Reflect.deleteProperty(navigator, "clipboard")
  Reflect.deleteProperty(document, "execCommand")
})

describe("StateBadge", () => {
  it("colours states as the TUI does", () => {
    expect(stateVariant("busy")).toBe("blue")
    expect(stateVariant("success")).toBe("green")
    expect(stateVariant("idle")).toBe("amber")
    expect(stateVariant("failure")).toBe("red")
    expect(stateVariant("cleaning")).toBe("secondary")
  })
  it("shows the state", () => {
    render(<StateBadge state="busy" />)
    expect(screen.getByText("busy")).toBeInTheDocument()
  })
})

describe("Sparkline", () => {
  it("breaks the line at gaps", () => {
    const { container } = render(<Sparkline label="running" values={[1, 2, null, 3, 4]} max={4} />)
    expect(screen.getByRole("img", { name: "running" })).toBeInTheDocument()
    expect(container.querySelectorAll("polyline")).toHaveLength(2)
  })
  it("draws nothing without values", () => {
    const { container } = render(<Sparkline label="cpu" values={[null, null]} />)
    expect(container.querySelectorAll("polyline")).toHaveLength(0)
  })
})

describe("copyText", () => {
  it("uses the Clipboard API in a secure context", async () => {
    secure(true)
    const writeText = vi.fn(async () => {})
    Object.defineProperty(navigator, "clipboard", { value: { writeText }, configurable: true })
    await copyText("aaaaaa")
    expect(writeText).toHaveBeenCalledWith("aaaaaa")
  })

  it("falls back to execCommand over plain HTTP", async () => {
    secure(false)
    const exec = vi.fn(() => true)
    Object.defineProperty(document, "execCommand", { value: exec, configurable: true })
    await copyText("bbbbbb")
    expect(exec).toHaveBeenCalledWith("copy")
    expect(document.querySelector("textarea")).toBeNull()
  })

  it("reports a refused copy", async () => {
    secure(false)
    Object.defineProperty(document, "execCommand", { value: () => false, configurable: true })
    await expect(copyText("cccccc")).rejects.toThrow("the browser refused to copy")
  })
})
```

- [ ] **Step 2: Run it to verify it fails**

Run: `timeout 300 npm --prefix web test -- src/components/components.test.tsx`
Expected: FAIL — `Failed to resolve import "@/lib/clipboard"`.

- [ ] **Step 3: Write the three modules**

`web/src/components/state-badge.tsx`:
```tsx
import { Badge, type BadgeVariant } from "darkraise-ui/components/badge"

const variants: Record<string, BadgeVariant> = {
  active: "green",
  online: "green",
  success: "green",
  matched: "green",
  ok: "green",
  busy: "blue",
  paused: "amber",
  idle: "amber",
  starting: "amber",
  "expires soon": "amber",
  unverified: "amber",
  error: "red",
  failure: "red",
  offline: "red",
  unmatched: "red",
  rejected: "red",
}

export function stateVariant(state: string): BadgeVariant {
  return variants[state] ?? "secondary"
}

export function StateBadge({ state }: { state: string }) {
  return (
    <Badge variant={stateVariant(state)} size="sm">
      {state}
    </Badge>
  )
}
```

`web/src/components/sparkline.tsx`:
```tsx
const W = 120
const H = 32

export function Sparkline({ values, max, label }: { values: (number | null)[]; max?: number; label: string }) {
  const nums = values.filter((v): v is number => v !== null)
  const top = max ?? Math.max(1, ...nums)
  const step = values.length > 1 ? W / (values.length - 1) : W
  const segments: string[][] = [[]]
  values.forEach((v, i) => {
    if (v === null) {
      if ((segments.at(-1) ?? []).length > 0) segments.push([])
      return
    }
    const y = H - 2 - (Math.min(v, top) / top) * (H - 4)
    segments.at(-1)?.push(`${(i * step).toFixed(1)},${y.toFixed(1)}`)
  })
  return (
    <svg role="img" aria-label={label} viewBox={`0 0 ${W} ${H}`} preserveAspectRatio="none" className="mt-2 h-8 w-full text-primary">
      {segments
        .filter((s) => s.length > 0)
        .map((s, i) => (
          <polyline key={i} fill="none" stroke="currentColor" strokeWidth="1.5" points={s.join(" ")} />
        ))}
    </svg>
  )
}
```

`web/src/lib/clipboard.ts`:
```ts
export async function copyText(text: string): Promise<void> {
  if (window.isSecureContext && navigator.clipboard) {
    await navigator.clipboard.writeText(text)
    return
  }
  // navigator.clipboard exists only on HTTPS or localhost; the web listener
  // is plain HTTP on the LAN, so fall back to a hidden textarea.
  const area = document.createElement("textarea")
  area.value = text
  area.setAttribute("readonly", "")
  area.style.position = "fixed"
  area.style.opacity = "0"
  document.body.appendChild(area)
  area.select()
  try {
    if (!document.execCommand("copy")) throw new Error("the browser refused to copy")
  } finally {
    area.remove()
  }
}
```

- [ ] **Step 4: Run the tests to verify they pass**

```bash
timeout 300 npm --prefix web test -- src/components/components.test.tsx
timeout 300 npm --prefix web run typecheck
timeout 300 npm --prefix web run lint
```
Expected: PASS (7 tests); typecheck and lint exit 0 (a `react-refresh/only-export-components` warning for `stateVariant` is acceptable).

- [ ] **Step 5: Commit**

```bash
git add web/src/components/state-badge.tsx web/src/components/sparkline.tsx web/src/lib/clipboard.ts web/src/components/components.test.tsx
git commit -m "feat(web): add state badge, sparkline and copy"
```

---

### Task 12: web: routes, auth gate and page stubs

**Files:**
- Create: `web/src/router.tsx`, `web/src/lib/router-adapter.tsx`
- Create (stubs, one shape): `login.tsx`, `dashboard.tsx`, `runners.tsx`, `runner-detail.tsx`, `history.tsx`, `repositories.tsx`, `storage.tsx`, `settings.tsx`, `not-yet.tsx`, all under `web/src/pages/`
- Test: `web/src/router.test.tsx`

**Interfaces:**
- Consumes: C5 `api`, `mockApi`; C6 `keys`.
- Produces: C9.

**Items:** 6

**Implementer:** dr-superpowers:impl-sonnet-high
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 2 = 5

- [ ] **Step 1: Write the failing test `web/src/router.test.tsx`**

```tsx
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { createMemoryHistory, RouterProvider } from "@tanstack/react-router"
import { render, screen, waitFor } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import { mockApi } from "@/test/api"
import { createAppRouter } from "./router"

const authed = { "GET /auth/state": { setup_required: false, authenticated: true } }
const anonymous = { "GET /auth/state": { setup_required: false, authenticated: false } }

function renderAt(path: string) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  const router = createAppRouter({ queryClient, history: createMemoryHistory({ initialEntries: [path] }) })
  render(
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  )
  return router
}

describe("routes", () => {
  it("sends a visitor without a session to /login", async () => {
    mockApi(anonymous)
    const router = renderAt("/runners")
    expect(await screen.findByRole("heading", { name: "Log in" })).toBeInTheDocument()
    expect(router.state.location.pathname).toBe("/login")
  })

  it("sends a logged-in visitor from /login to the dashboard", async () => {
    mockApi(authed)
    const router = renderAt("/login")
    expect(await screen.findByRole("heading", { name: "Dashboard" })).toBeInTheDocument()
    expect(router.state.location.pathname).toBe("/")
  })

  it.each([
    ["/runners", "Runners"],
    ["/history", "History"],
    ["/repositories", "Repositories"],
    ["/storage", "Storage"],
    ["/settings", "Settings"],
    ["/runners/aaaaaa", "Runner"],
  ])("serves %s", async (path, title) => {
    mockApi(authed)
    renderAt(path)
    expect(await screen.findByRole("heading", { name: title })).toBeInTheDocument()
  })

  it("shows the later pages as not in the web UI yet", async () => {
    mockApi(authed)
    renderAt("/storage")
    expect(await screen.findByText("Not in the web UI yet")).toBeInTheDocument()
  })

  it("reads the runner detail tab from the search", async () => {
    mockApi(authed)
    const router = renderAt("/runners/aaaaaa?tab=log")
    await waitFor(() => expect(router.state.location.search).toEqual({ tab: "log" }))
  })

  it("falls back to the steps tab for an unknown tab", async () => {
    mockApi(authed)
    const router = renderAt("/runners/aaaaaa?tab=bogus")
    await waitFor(() => expect(router.state.location.search).toEqual({ tab: "steps" }))
  })
})
```

- [ ] **Step 2: Run it to verify it fails**

Run: `timeout 300 npm --prefix web test -- src/router.test.tsx`
Expected: FAIL — `Failed to resolve import "./router"`.

- [ ] **Step 3: Write the page stubs**

`web/src/pages/not-yet.tsx`:
```tsx
import { EmptyState } from "darkraise-ui/components/empty-state"
import { PageHeader } from "darkraise-ui/layout"

export function NotYet({ title }: { title: string }) {
  return (
    <>
      <PageHeader title={title} />
      <EmptyState
        title="Not in the web UI yet"
        description="A later ghr release adds this page. Until then, run ghr on the runner host to use it in the terminal."
      />
    </>
  )
}
```

`web/src/pages/repositories.tsx`:
```tsx
import { NotYet } from "./not-yet"

export function RepositoriesPage() {
  return <NotYet title="Repositories" />
}
```

`web/src/pages/storage.tsx`:
```tsx
import { NotYet } from "./not-yet"

export function StoragePage() {
  return <NotYet title="Storage" />
}
```

`web/src/pages/settings.tsx`:
```tsx
import { NotYet } from "./not-yet"

export function SettingsPage() {
  return <NotYet title="Settings" />
}
```

`web/src/pages/login.tsx` (Task 15 replaces it):
```tsx
export function LoginPage() {
  return <h1>Log in</h1>
}
```

`web/src/pages/dashboard.tsx` (Task 17 replaces it):
```tsx
import { PageHeader } from "darkraise-ui/layout"

export function DashboardPage() {
  return <PageHeader title="Dashboard" />
}
```

`web/src/pages/runners.tsx` (Task 16 replaces it):
```tsx
import { PageHeader } from "darkraise-ui/layout"

export function RunnersPage() {
  return <PageHeader title="Runners" />
}
```

`web/src/pages/history.tsx` (Task 19 replaces it):
```tsx
import { PageHeader } from "darkraise-ui/layout"

export function HistoryPage() {
  return <PageHeader title="History" />
}
```

`web/src/pages/runner-detail.tsx` (Task 18 replaces the component and keeps the type):
```tsx
import { PageHeader } from "darkraise-ui/layout"

export type DetailTab = "steps" | "log" | "containers"

export function RunnerDetailPage() {
  return <PageHeader title="Runner" />
}
```

- [ ] **Step 4: Write the router adapter `web/src/lib/router-adapter.tsx`**

```tsx
import { Link, useNavigate, useRouter, useRouterState } from "@tanstack/react-router"
import type { RouterAdapter, RouterLinkProps } from "darkraise-ui/router"

function RouterLink({ to, activeClassName, activeExact, ...rest }: RouterLinkProps) {
  return (
    <Link
      to={to}
      activeOptions={activeExact ? { exact: true } : undefined}
      activeProps={activeClassName ? { className: activeClassName } : undefined}
      {...rest}
    />
  )
}

export const routerAdapter: RouterAdapter = {
  Link: RouterLink,
  useNavigate: () => {
    const navigate = useNavigate()
    return (to: string) => {
      void navigate({ to })
    }
  },
  usePathname: () => useRouterState({ select: (s) => s.location.pathname }),
  useBack: () => {
    const router = useRouter()
    return () => router.history.back()
  },
  useInvalidate: () => {
    const router = useRouter()
    return () => {
      void router.invalidate()
    }
  },
}
```

- [ ] **Step 5: Write `web/src/router.tsx`**

```tsx
import type { QueryClient } from "@tanstack/react-query"
import {
  createRootRouteWithContext,
  createRoute,
  createRouter,
  Outlet,
  redirect,
  type RouterHistory,
} from "@tanstack/react-router"
import { RouterAdapterProvider } from "darkraise-ui/router"
import { api } from "./api/client"
import { keys } from "./api/hooks"
import { routerAdapter } from "./lib/router-adapter"
import { DashboardPage } from "./pages/dashboard"
import { HistoryPage } from "./pages/history"
import { LoginPage } from "./pages/login"
import { RepositoriesPage } from "./pages/repositories"
import { RunnerDetailPage, type DetailTab } from "./pages/runner-detail"
import { RunnersPage } from "./pages/runners"
import { SettingsPage } from "./pages/settings"
import { StoragePage } from "./pages/storage"

// staleTime 0: every navigation into or out of the gated pages asks the
// daemon again, so a logout or an expired session is seen at once.
function authState(queryClient: QueryClient) {
  return queryClient.fetchQuery({ queryKey: keys.auth, queryFn: ({ signal }) => api.authState(signal), staleTime: 0 })
}

function Root() {
  return (
    <RouterAdapterProvider value={routerAdapter}>
      <Outlet />
    </RouterAdapterProvider>
  )
}

function AppFrame() {
  return <Outlet />
}

const rootRoute = createRootRouteWithContext<{ queryClient: QueryClient }>()({ component: Root })

const loginRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/login",
  beforeLoad: async ({ context }) => {
    if ((await authState(context.queryClient)).authenticated) throw redirect({ to: "/" })
  },
  component: LoginPage,
})

const appRoute = createRoute({
  getParentRoute: () => rootRoute,
  id: "app",
  beforeLoad: async ({ context }) => {
    if (!(await authState(context.queryClient)).authenticated) throw redirect({ to: "/login" })
  },
  component: AppFrame,
})

const dashboardRoute = createRoute({ getParentRoute: () => appRoute, path: "/", component: DashboardPage })
const repositoriesRoute = createRoute({ getParentRoute: () => appRoute, path: "/repositories", component: RepositoriesPage })
const runnersRoute = createRoute({ getParentRoute: () => appRoute, path: "/runners", component: RunnersPage })
const runnerRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/runners/$id",
  validateSearch: (search: Record<string, unknown>): { tab: DetailTab } => ({
    tab: search.tab === "log" || search.tab === "containers" ? search.tab : "steps",
  }),
  component: RunnerDetailPage,
})
const historyRoute = createRoute({ getParentRoute: () => appRoute, path: "/history", component: HistoryPage })
const storageRoute = createRoute({ getParentRoute: () => appRoute, path: "/storage", component: StoragePage })
const settingsRoute = createRoute({ getParentRoute: () => appRoute, path: "/settings", component: SettingsPage })

const routeTree = rootRoute.addChildren([
  loginRoute,
  appRoute.addChildren([dashboardRoute, repositoriesRoute, runnersRoute, runnerRoute, historyRoute, storageRoute, settingsRoute]),
])

export function createAppRouter({ queryClient, history }: { queryClient: QueryClient; history?: RouterHistory }) {
  return createRouter({ routeTree, context: { queryClient }, history })
}

export type AppRouter = ReturnType<typeof createAppRouter>

declare module "@tanstack/react-router" {
  interface Register {
    router: AppRouter
  }
}
```

- [ ] **Step 6: Run the tests and checks to verify they pass**

```bash
timeout 300 npm --prefix web test -- src/router.test.tsx
timeout 300 npm --prefix web run typecheck
timeout 300 npm --prefix web run lint
timeout 300 npm --prefix web test
```
Expected: PASS (11 tests in `router.test.tsx`); typecheck, lint and the full suite exit 0. If `tsc` rejects the adapter's plain-string `to` against the registered route types (see Assumptions), cast it at both adapter call sites only — `to={to as never}` in `RouterLink` and `navigate({ to: to as never })` in `useNavigate` — and leave the route definitions as they are.

- [ ] **Step 7: Commit**

```bash
git add web/src/router.tsx web/src/router.test.tsx web/src/lib/router-adapter.tsx web/src/pages
git commit -m "feat(web): add routes, auth gate and page stubs"
```

---

### Task 13: web: app providers and test renderer

**Files:**
- Create: `web/src/query.ts`
- Modify (rewrite): `web/src/app.tsx`, `web/src/main.tsx`
- Test: `web/src/app.test.tsx` (rewrite), `web/src/test/render.tsx`

**Interfaces:**
- Consumes: C5 `ApiError`, `setUnauthorizedHandler`, `mockApi`, `json`; C4 `authedRoutes`; C7 `hhmm`; C9 `createAppRouter`, `AppRouter`.
- Produces: C10.

**Items:** 6

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 1 = 4

- [ ] **Step 1: Write the renderer `web/src/test/render.tsx`**

```tsx
import { createMemoryHistory } from "@tanstack/react-router"
import { render } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { App } from "@/app"
import { createQueryClient } from "@/query"
import { createAppRouter } from "@/router"

export function renderApp(path: string) {
  const queryClient = createQueryClient()
  const router = createAppRouter({ queryClient, history: createMemoryHistory({ initialEntries: [path] }) })
  const user = userEvent.setup()
  const view = render(<App router={router} queryClient={queryClient} />)
  return { ...view, router, queryClient, user }
}
```

- [ ] **Step 2: Rewrite the test `web/src/app.test.tsx`**

Replace the whole file (the three `index.html` tests from Task 4 stay):
```tsx
import { readFileSync } from "node:fs"
import { fileURLToPath } from "node:url"
import { MutationObserver } from "@tanstack/react-query"
import { act, screen, waitFor } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import { ApiError, api } from "./api/client"
import { errorText } from "./query"
import { json, mockApi } from "./test/api"
import { authedRoutes } from "./test/fixtures"
import { renderApp } from "./test/render"

describe("App", () => {
  it("renders the routed page inside the providers", async () => {
    mockApi(authedRoutes())
    renderApp("/")
    expect(await screen.findByRole("heading", { name: "Dashboard" })).toBeInTheDocument()
  })

  it("sends an expired session from any API call to /login", async () => {
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
    await act(async () => {
      await api.status().catch(() => undefined)
    })
    await waitFor(() => expect(router.state.location.pathname).toBe("/login"))
  })

  it("toasts a failed mutation", async () => {
    mockApi(authedRoutes())
    const { queryClient } = renderApp("/")
    await screen.findByRole("heading", { name: "Dashboard" })
    await act(async () => {
      await new MutationObserver(queryClient, { mutationFn: () => Promise.reject(new Error("boom")) })
        .mutate()
        .catch(() => undefined)
    })
    expect((await screen.findAllByText("boom")).length).toBeGreaterThan(0)
  })
})

describe("errorText", () => {
  it("adds the retry time of a rate-limited request", () => {
    const err = new ApiError(429, "GitHub rate limit; API calls are paused", new Date("2026-10-06T14:20:00Z"))
    expect(errorText(err)).toBe("GitHub rate limit; API calls are paused — retry after 14:20")
  })
  it("passes other errors through", () => {
    expect(errorText(new Error("boom"))).toBe("boom")
    expect(errorText("plain")).toBe("plain")
  })
})

describe("index.html", () => {
  const html = readFileSync(fileURLToPath(new URL("../index.html", import.meta.url)), "utf8")

  it("has no inline script, which the CSP forbids", () => {
    expect(html).not.toMatch(/<script(?![^>]*\bsrc=)[^>]*>/)
  })

  it("loads nothing from another origin", () => {
    expect(html).not.toMatch(/https?:\/\//)
  })

  it("is titled ghr", () => {
    expect(html).toContain("<title>ghr</title>")
  })
})
```

- [ ] **Step 3: Run it to verify it fails**

Run: `timeout 300 npm --prefix web test -- src/app.test.tsx`
Expected: FAIL — `Failed to resolve import "@/query"` (or `"./query"`).

- [ ] **Step 4: Write `web/src/query.ts`, then rewrite `app.tsx` and `main.tsx`**

`web/src/query.ts`:
```ts
import { MutationCache, QueryClient } from "@tanstack/react-query"
import { toast } from "darkraise-ui/components/sonner"
import { ApiError } from "./api/client"
import { hhmm } from "./lib/format"

export function errorText(err: unknown): string {
  if (err instanceof ApiError && err.retryAt) return `${err.message} — retry after ${hhmm(err.retryAt)}`
  return err instanceof Error ? err.message : String(err)
}

// Polls do not retry: the next poll is the retry, as in the TUI. A failed
// action shows as a toast, whichever page started it.
export function createQueryClient(): QueryClient {
  return new QueryClient({
    defaultOptions: { queries: { retry: false } },
    mutationCache: new MutationCache({ onError: (err) => toast.error(errorText(err)) }),
  })
}
```

`web/src/app.tsx`:
```tsx
import { QueryClientProvider, type QueryClient } from "@tanstack/react-query"
import { RouterProvider } from "@tanstack/react-router"
import { Toaster } from "darkraise-ui/components/sonner"
import { ThemeProvider } from "darkraise-ui/theme"
import { useEffect } from "react"
import { setUnauthorizedHandler } from "./api/client"
import type { AppRouter } from "./router"
import { themeConfig } from "./theme.config"

export function App({ router, queryClient }: { router: AppRouter; queryClient: QueryClient }) {
  useEffect(() => {
    setUnauthorizedHandler(() => {
      queryClient.clear()
      void router.navigate({ to: "/login" })
    })
  }, [router, queryClient])
  return (
    <QueryClientProvider client={queryClient}>
      <ThemeProvider config={themeConfig}>
        <RouterProvider router={router} />
        <Toaster />
      </ThemeProvider>
    </QueryClientProvider>
  )
}
```

`web/src/main.tsx`:
```tsx
import "@fontsource/inter/400.css"
import "@fontsource/inter/500.css"
import "@fontsource/inter/600.css"
import "@fontsource/inter/700.css"
import "@fontsource/jetbrains-mono/400.css"
import "@fontsource/jetbrains-mono/500.css"
import "./styles/globals.css"
import { StrictMode } from "react"
import { createRoot } from "react-dom/client"
import { App } from "./app"
import { createQueryClient } from "./query"
import { createAppRouter } from "./router"

const root = document.getElementById("root")
if (!root) throw new Error("index.html has no #root element")
const queryClient = createQueryClient()
const router = createAppRouter({ queryClient })
createRoot(root).render(
  <StrictMode>
    <App router={router} queryClient={queryClient} />
  </StrictMode>,
)
```

- [ ] **Step 5: Run the tests and checks to verify they pass**

```bash
timeout 300 npm --prefix web test -- src/app.test.tsx
timeout 300 npm --prefix web run typecheck
timeout 300 npm --prefix web run lint
timeout 600 npm --prefix web run build
git status --porcelain web/dist
```
Expected: PASS (8 tests); typecheck and lint exit 0; the build succeeds and `git status` prints nothing for `web/dist`.

- [ ] **Step 6: Commit**

```bash
git add web/src/query.ts web/src/app.tsx web/src/main.tsx web/src/app.test.tsx web/src/test/render.tsx
git commit -m "feat(web): wire the query client and router"
```

---

### Task 14: web: shell with banners and log out

**Files:**
- Create: `web/src/components/shell.tsx`
- Modify: `web/src/router.tsx` (the `app` route renders `Shell`; `AppFrame` goes)
- Test: `web/src/components/shell.test.tsx`; `web/src/router.test.tsx` (rewrite, so it renders through the app's providers)

**Interfaces:**
- Consumes: C5 `api`, `ApiError`, `mockApi`, `json`, `noContent`; C4 `fixtures`, `authedRoutes`; C6 `keys`, `useStatus`, `useConfig`, `useMetrics`, `useEvents`; C10 `errorText`, `renderApp`.
- Produces: C11.

**Items:** 6

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 1 = 4

- [ ] **Step 1: Write the failing test `web/src/components/shell.test.tsx`**

```tsx
import { screen, waitFor } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"
import { keys } from "@/api/hooks"
import { json, mockApi, noContent } from "@/test/api"
import { authedRoutes, fixtures } from "@/test/fixtures"
import { renderApp } from "@/test/render"

describe("shell", () => {
  it("lists the six pages in the sidebar", async () => {
    mockApi(authedRoutes())
    renderApp("/")
    for (const label of ["Dashboard", "Repositories", "Runners", "History", "Storage", "Settings"]) {
      expect((await screen.findAllByRole("link", { name: label })).length).toBeGreaterThan(0)
    }
  })

  it("shows reconnecting while status polls fail, until one succeeds", async () => {
    let fail = true
    mockApi(authedRoutes({ "GET /api/status": () => (fail ? json({ error: "connection refused" }, 502) : fixtures.status) }))
    renderApp("/")
    expect(await screen.findByText("daemon unreachable: connection refused — retrying")).toBeInTheDocument()
    fail = false
    await waitFor(() => expect(screen.queryByText(/daemon unreachable/)).toBeNull(), { timeout: 3000 })
  })

  it("shows why the daemon is degraded", async () => {
    mockApi(authedRoutes({ "GET /api/status": { ...fixtures.status, degraded: true, degraded_reason: "GitHub rejected the token" } }))
    renderApp("/")
    expect(await screen.findByText("DEGRADED: GitHub rejected the token — no new runners")).toBeInTheDocument()
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
    await screen.findByText("Not in the web UI yet")
    await waitFor(() => {
      for (const path of ["/api/config", "/api/metrics", "/api/events"]) {
        expect(calls.some((c) => c.path === path)).toBe(true)
      }
    })
  })

  it("logs out from the user menu", async () => {
    let authenticated = true
    const { calls } = mockApi(
      authedRoutes({
        "GET /auth/state": () => ({ setup_required: false, authenticated }),
        "POST /auth/logout": () => {
          authenticated = false
          return noContent()
        },
      }),
    )
    const { user, router } = renderApp("/")
    await user.click(await screen.findByRole("button", { name: "O" }))
    await user.click(await screen.findByText("Log out"))
    await waitFor(() => expect(router.state.location.pathname).toBe("/login"))
    expect(calls.some((c) => c.method === "POST" && c.path === "/auth/logout")).toBe(true)
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
    expect(screen.queryByText(/daemon unreachable/)).toBeNull()
  })
})
```

- [ ] **Step 2: Run it to verify it fails**

Run: `timeout 300 npm --prefix web test -- src/components/shell.test.tsx`
Expected: FAIL — `Unable to find role="link" and name "Dashboard"` (the `app` route still renders the bare `AppFrame`), and the banner and log-out tests fail the same way.

- [ ] **Step 3: Write `web/src/components/shell.tsx`**

```tsx
import { useQueryClient } from "@tanstack/react-query"
import { Outlet, useNavigate } from "@tanstack/react-router"
import { Alert, AlertDescription, AlertTitle } from "darkraise-ui/components/alert"
import { SidebarLayout, useBrandStore, type NavGroup } from "darkraise-ui/layout"
import { Boxes, GitBranch, HardDrive, HistoryIcon, LayoutDashboard, Settings } from "lucide-react"
import { useEffect, useRef } from "react"
import { ApiError, api } from "@/api/client"
import { keys, useConfig, useEvents, useMetrics, useStatus } from "@/api/hooks"
import { errorText } from "@/query"

const nav: NavGroup[] = [
  {
    items: [
      { label: "Dashboard", href: "/", icon: LayoutDashboard },
      { label: "Repositories", href: "/repositories", icon: GitBranch },
      { label: "Runners", href: "/runners", icon: Boxes },
      { label: "History", href: "/history", icon: HistoryIcon },
      { label: "Storage", href: "/storage", icon: HardDrive },
      { label: "Settings", href: "/settings", icon: Settings },
    ],
  },
]

export function Shell() {
  const status = useStatus()
  const queryClient = useQueryClient()
  const navigate = useNavigate()
  const epoch = status.data?.epoch
  const seenEpoch = useRef<string | undefined>(undefined)
  // Mounted here, not per page, so config, metrics and the event feed poll
  // on every page as the TUI's do, and the Dashboard opens with them warm.
  useConfig()
  useMetrics()
  useEvents(epoch)

  useEffect(() => {
    useBrandStore.getState().setAppName("ghr")
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
    await navigate({ to: "/login" })
  }

  const unreachable = status.isError && !(status.error instanceof ApiError && status.error.status === 401)
  return (
    <SidebarLayout nav={nav} notificationSlot={null} user={{ name: "Owner", email: "ghr" }} onLogout={() => void logout()}>
      {unreachable && (
        <Alert variant="destructive" className="mb-4">
          <AlertTitle>Reconnecting</AlertTitle>
          <AlertDescription>daemon unreachable: {errorText(status.error)} — retrying</AlertDescription>
        </Alert>
      )}
      {status.data?.degraded && (
        <Alert variant="warning" className="mb-4">
          <AlertTitle>Degraded</AlertTitle>
          <AlertDescription>DEGRADED: {status.data.degraded_reason} — no new runners</AlertDescription>
        </Alert>
      )}
      <Outlet />
    </SidebarLayout>
  )
}
```

- [ ] **Step 4: Render the shell from the `app` route**

In `web/src/router.tsx`, add the import (keep the imports sorted by path):
```tsx
import { Shell } from "./components/shell"
```
delete the whole `AppFrame` function:
```tsx
function AppFrame() {
  return <Outlet />
}
```
and in `appRoute` change `component: AppFrame,` to `component: Shell,`. `Outlet` stays imported (the root route still uses it).

- [ ] **Step 5: Render the router tests through the app's providers**

`SidebarLayout` renders a `ThemeSwitcher`, which throws `useTheme must be used within a ThemeProvider` without one, and Task 12's `renderAt` wraps the router in `QueryClientProvider` only. Replace the whole of `web/src/router.test.tsx` with this version, whose `renderAt` goes through `renderApp` (Task 13); the tests themselves are unchanged:
```tsx
import { screen, waitFor } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import { mockApi } from "@/test/api"
import { renderApp } from "@/test/render"

const authed = { "GET /auth/state": { setup_required: false, authenticated: true } }
const anonymous = { "GET /auth/state": { setup_required: false, authenticated: false } }

function renderAt(path: string) {
  return renderApp(path).router
}

describe("routes", () => {
  it("sends a visitor without a session to /login", async () => {
    mockApi(anonymous)
    const router = renderAt("/runners")
    expect(await screen.findByRole("heading", { name: "Log in" })).toBeInTheDocument()
    expect(router.state.location.pathname).toBe("/login")
  })

  it("sends a logged-in visitor from /login to the dashboard", async () => {
    mockApi(authed)
    const router = renderAt("/login")
    expect(await screen.findByRole("heading", { name: "Dashboard" })).toBeInTheDocument()
    expect(router.state.location.pathname).toBe("/")
  })

  it.each([
    ["/runners", "Runners"],
    ["/history", "History"],
    ["/repositories", "Repositories"],
    ["/storage", "Storage"],
    ["/settings", "Settings"],
    ["/runners/aaaaaa", "Runner"],
  ])("serves %s", async (path, title) => {
    mockApi(authed)
    renderAt(path)
    expect(await screen.findByRole("heading", { name: title })).toBeInTheDocument()
  })

  it("shows the later pages as not in the web UI yet", async () => {
    mockApi(authed)
    renderAt("/storage")
    expect(await screen.findByText("Not in the web UI yet")).toBeInTheDocument()
  })

  it("reads the runner detail tab from the search", async () => {
    mockApi(authed)
    const router = renderAt("/runners/aaaaaa?tab=log")
    await waitFor(() => expect(router.state.location.search).toEqual({ tab: "log" }))
  })

  it("falls back to the steps tab for an unknown tab", async () => {
    mockApi(authed)
    const router = renderAt("/runners/aaaaaa?tab=bogus")
    await waitFor(() => expect(router.state.location.search).toEqual({ tab: "steps" }))
  })
})
```
These tests mock only `/auth/state`; the shell's status, config, metrics and events polls get the mock's 404 and the pages still render their headings.

- [ ] **Step 6: Run the tests and checks to verify they pass**

```bash
timeout 300 npm --prefix web test -- src/components/shell.test.tsx
timeout 300 npm --prefix web test
timeout 300 npm --prefix web run typecheck
timeout 300 npm --prefix web run lint
```
Expected: PASS (7 tests in `shell.test.tsx`, and the full suite, including the rewritten `router.test.tsx`); typecheck and lint exit 0.

- [ ] **Step 7: Commit**

```bash
git add web/src/components/shell.tsx web/src/components/shell.test.tsx web/src/router.tsx web/src/router.test.tsx
git commit -m "feat(web): add the sidebar shell and log out"
```

---

### Task 15: web: login and setup page

**Files:**
- Modify (rewrite): `web/src/pages/login.tsx`
- Test: `web/src/pages/login.test.tsx`

**Interfaces:**
- Consumes: C5 `api`, `ApiError`, `mockApi`, `json`, `noContent`; C4 `authedRoutes`; C6 `keys`; C7 `hhmm`; C10 `renderApp`.
- Produces: nothing other tasks read.

**Items:** 5

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 1 = 3

- [ ] **Step 1: Write the failing test `web/src/pages/login.test.tsx`**

```tsx
import { screen, waitFor } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import { json, mockApi, noContent } from "@/test/api"
import { authedRoutes } from "@/test/fixtures"
import { renderApp } from "@/test/render"

const loginState = { "GET /auth/state": { setup_required: false, authenticated: false } }

describe("login page", () => {
  it("sets the first password, checking it first, and opens the dashboard", async () => {
    let done = false
    const { calls } = mockApi(
      authedRoutes({
        "GET /auth/state": () => ({ setup_required: !done, authenticated: done }),
        "POST /auth/setup": () => {
          done = true
          return noContent()
        },
      }),
    )
    const { user, router } = renderApp("/login")
    const password = await screen.findByLabelText("Password")
    const confirm = screen.getByLabelText("Confirm password")
    const submit = screen.getByRole("button", { name: "Set password" })

    await user.type(password, "short")
    await user.type(confirm, "short")
    await user.click(submit)
    expect(screen.getByText("password must be 12 to 1024 bytes")).toBeInTheDocument()

    await user.clear(password)
    await user.clear(confirm)
    await user.type(password, "correct horse battery")
    await user.type(confirm, "correct horse batteries")
    await user.click(submit)
    expect(screen.getByText("the passwords do not match")).toBeInTheDocument()
    expect(calls.some((c) => c.path === "/auth/setup")).toBe(false)

    await user.clear(confirm)
    await user.type(confirm, "correct horse battery")
    await user.click(submit)
    await waitFor(() => expect(router.state.location.pathname).toBe("/"))
    expect(calls.find((c) => c.path === "/auth/setup")?.body).toEqual({ password: "correct horse battery" })
  })

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

  it("shows a wrong password inline", async () => {
    mockApi({ ...loginState, "POST /auth/login": () => json({ error: "wrong password" }, 401) })
    const { user, router } = renderApp("/login")
    await user.type(await screen.findByLabelText("Password"), "not the password")
    await user.click(screen.getByRole("button", { name: "Log in" }))
    expect(await screen.findByText("wrong password")).toBeInTheDocument()
    expect(router.state.location.pathname).toBe("/login")
  })

  it("says when to retry after too many failures", async () => {
    mockApi({
      ...loginState,
      "POST /auth/login": () =>
        json({ error: "too many failed logins; retry after 2026-10-06T14:20:00Z", retry_at: "2026-10-06T14:20:00Z" }, 429),
    })
    const { user } = renderApp("/login")
    await user.type(await screen.findByLabelText("Password"), "not the password")
    await user.click(screen.getByRole("button", { name: "Log in" }))
    expect(await screen.findByText("Too many failed logins; try again after 14:20.")).toBeInTheDocument()
  })
})
```

- [ ] **Step 2: Run it to verify it fails**

Run: `timeout 300 npm --prefix web test -- src/pages/login.test.tsx`
Expected: FAIL — `Unable to find a label with the text of: Password` (the stub has no form).

- [ ] **Step 3: Write `web/src/pages/login.tsx`**

```tsx
import { useQuery, useQueryClient } from "@tanstack/react-query"
import { useNavigate } from "@tanstack/react-router"
import { Button } from "darkraise-ui/components/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "darkraise-ui/components/card"
import { Input } from "darkraise-ui/components/input"
import { Label } from "darkraise-ui/components/label"
import { Spinner } from "darkraise-ui/components/spinner"
import { useState, type FormEvent } from "react"
import { ApiError, api } from "@/api/client"
import { keys } from "@/api/hooks"
import { hhmm } from "@/lib/format"

const MIN_BYTES = 12
const MAX_BYTES = 1024

function lengthOk(password: string): boolean {
  const n = new TextEncoder().encode(password).length
  return n >= MIN_BYTES && n <= MAX_BYTES
}

function loginError(err: unknown): string {
  if (err instanceof ApiError && err.status === 429 && err.retryAt) {
    return `Too many failed logins; try again after ${hhmm(err.retryAt)}.`
  }
  return err instanceof Error ? err.message : String(err)
}

export function LoginPage() {
  const auth = useQuery({ queryKey: keys.auth, queryFn: ({ signal }) => api.authState(signal) })
  const queryClient = useQueryClient()
  const navigate = useNavigate()
  const [password, setPassword] = useState("")
  const [confirm, setConfirm] = useState("")
  const [error, setError] = useState("")
  const [busy, setBusy] = useState(false)

  if (auth.isError) {
    return <p className="p-8 text-center text-sm text-destructive">cannot reach ghr: {loginError(auth.error)}</p>
  }
  if (!auth.data) {
    return (
      <div className="flex min-h-screen items-center justify-center">
        <Spinner />
      </div>
    )
  }
  const setup = auth.data.setup_required

  async function submit(e: FormEvent) {
    e.preventDefault()
    setError("")
    if (setup && !lengthOk(password)) return setError("password must be 12 to 1024 bytes")
    if (setup && password !== confirm) return setError("the passwords do not match")
    setBusy(true)
    try {
      await (setup ? api.setup(password) : api.login(password))
      await queryClient.invalidateQueries({ queryKey: keys.auth })
      await navigate({ to: "/" })
    } catch (err) {
      setError(loginError(err))
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="flex min-h-screen items-center justify-center p-4">
      <Card className="w-full max-w-sm">
        <CardHeader>
          <CardTitle>ghr</CardTitle>
          <CardDescription>
            {setup
              ? "Choose the web password. The first visitor sets it, so do this now."
              : "Log in to manage this runner host."}
          </CardDescription>
        </CardHeader>
        <CardContent>
          <form className="flex flex-col gap-4" onSubmit={(e) => void submit(e)}>
            <div className="flex flex-col gap-2">
              <Label htmlFor="password">Password</Label>
              <Input
                id="password"
                type="password"
                autoComplete={setup ? "new-password" : "current-password"}
                value={password}
                onChange={(e) => setPassword(e.target.value)}
              />
            </div>
            {setup && (
              <div className="flex flex-col gap-2">
                <Label htmlFor="confirm">Confirm password</Label>
                <Input
                  id="confirm"
                  type="password"
                  autoComplete="new-password"
                  value={confirm}
                  onChange={(e) => setConfirm(e.target.value)}
                />
              </div>
            )}
            {error && <p className="text-sm text-destructive">{error}</p>}
            <Button type="submit" loading={busy}>
              {setup ? "Set password" : "Log in"}
            </Button>
          </form>
        </CardContent>
      </Card>
    </div>
  )
}
```

- [ ] **Step 4: Point the router test at the real login page**

The stub's `<h1>Log in</h1>` is gone. In `web/src/router.test.tsx`, test `sends a visitor without a session to /login`, replace
```tsx
    expect(await screen.findByRole("heading", { name: "Log in" })).toBeInTheDocument()
```
with
```tsx
    expect(await screen.findByRole("button", { name: "Log in" })).toBeInTheDocument()
```

- [ ] **Step 5: Run the tests and checks to verify they pass**

```bash
timeout 300 npm --prefix web test -- src/pages/login.test.tsx
timeout 300 npm --prefix web test
timeout 300 npm --prefix web run typecheck
timeout 300 npm --prefix web run lint
```
Expected: PASS (4 tests, and the full suite); typecheck and lint exit 0.

- [ ] **Step 6: Commit**

```bash
git add web/src/pages/login.tsx web/src/pages/login.test.tsx web/src/router.test.tsx
git commit -m "feat(web): add the login and setup page"
```

---

### Task 16: web: Runners page

**Files:**
- Create: `web/src/components/runners-table.tsx`
- Modify (rewrite): `web/src/pages/runners.tsx`
- Test: `web/src/pages/runners.test.tsx`

**Interfaces:**
- Consumes: C4 types, `fixtures`, `authedRoutes`; C5 `api`, `mockApi`, `noContent`; C6 `keys`, `useStatus`; C7 `elapsed`, `useNow`; C8 `StateBadge`, `copyText`; C10 `errorText`, `renderApp`.
- Produces: C12.

**Items:** 8

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 1 = 4

- [ ] **Step 1: Write the failing test `web/src/pages/runners.test.tsx`**

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

    await user.click(row.getByRole("button", { name: "Stop" }))
    expect(await screen.findByText("Runner aaaaaa is running a job. Stop it?")).toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: "Cancel" }))
    expect(calls.some((c) => c.method === "DELETE")).toBe(false)

    await user.click(row.getByRole("button", { name: "Stop" }))
    await user.click(within(await screen.findByRole("alertdialog")).getByRole("button", { name: "Stop" }))
    await waitFor(() => expect(calls.some((c) => c.method === "DELETE" && c.path === "/api/runners/aaaaaa")).toBe(true))
    await waitFor(() => expect(screen.queryByRole("alertdialog")).toBeNull())
    expect((await screen.findAllByText("stopped aaaaaa")).length).toBeGreaterThan(0)
  })

  it("asks plainly before stopping an idle runner", async () => {
    mockApi(authedRoutes())
    const { user } = renderApp("/runners")
    await user.click((await rowOf("bbbbbb")).getByRole("button", { name: "Stop" }))
    expect(await screen.findByText("Stop runner bbbbbb?")).toBeInTheDocument()
  })

  it("copies a runner ID", async () => {
    Object.defineProperty(window, "isSecureContext", { value: true, configurable: true })
    const writeText = vi.fn(async () => {})
    Object.defineProperty(navigator, "clipboard", { value: { writeText }, configurable: true })
    mockApi(authedRoutes())
    const { user } = renderApp("/runners")
    await user.click((await rowOf("aaaaaa")).getByRole("button", { name: "Copy ID" }))
    expect(writeText).toHaveBeenCalledWith("aaaaaa")
    expect((await screen.findAllByText("copied aaaaaa")).length).toBeGreaterThan(0)
    Reflect.deleteProperty(navigator, "clipboard")
  })

  it("opens the log tab from Logs", async () => {
    mockApi(authedRoutes())
    const { user, router } = renderApp("/runners")
    await user.click((await rowOf("aaaaaa")).getByRole("button", { name: "Logs" }))
    await waitFor(() => expect(router.state.location.pathname).toBe("/runners/aaaaaa"))
    expect(router.state.location.search).toEqual({ tab: "log" })
  })
})
```

- [ ] **Step 2: Run it to verify it fails**

Run: `timeout 300 npm --prefix web test -- src/pages/runners.test.tsx`
Expected: FAIL — `Unable to find role="link" and name "aaaaaa"`.

- [ ] **Step 3: Write `web/src/components/runners-table.tsx`**

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
import { keys } from "@/api/hooks"
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
                      onClick={() => void navigate({ to: "/runners/$id", params: { id: i.id }, search: { tab: "log" } })}
                    >
                      Logs
                    </Button>
                    <Button size="sm" variant="outline" onClick={() => void copy(i.id)}>
                      Copy ID
                    </Button>
                    <Button size="sm" variant="destructive" onClick={() => setStopping(i)}>
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
  const stop = useMutation({
    mutationFn: (id: string) => api.stopRunner(id),
    onSuccess: (_data, id) => {
      toast.success(`stopped ${id}`)
    },
    onSettled: () => queryClient.invalidateQueries({ queryKey: keys.status }),
  })
  const busy = instance?.state === "busy"
  return (
    <AlertDialog
      open={instance !== null}
      onOpenChange={(open) => {
        if (!open) onClose()
      }}
    >
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>
            {busy ? `Runner ${instance?.id} is running a job. Stop it?` : `Stop runner ${instance?.id}?`}
          </AlertDialogTitle>
          <AlertDialogDescription>
            {busy ? "The job it is running fails." : "ghr stops the runner and cleans it up."}
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>Cancel</AlertDialogCancel>
          <AlertDialogAction
            data-variant="destructive"
            onClick={() => {
              if (instance) stop.mutate(instance.id)
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

- [ ] **Step 4: Rewrite `web/src/pages/runners.tsx`**

```tsx
import { Card, CardContent } from "darkraise-ui/components/card"
import { Spinner } from "darkraise-ui/components/spinner"
import { PageHeader } from "darkraise-ui/layout"
import { useStatus } from "@/api/hooks"
import { RunnersTable } from "@/components/runners-table"

export function RunnersPage() {
  const status = useStatus()
  return (
    <>
      <PageHeader title="Runners" />
      <Card>
        <CardContent className="p-4">
          {status.data ? <RunnersTable status={status.data} actions /> : <Spinner label="waiting for the daemon…" />}
        </CardContent>
      </Card>
    </>
  )
}
```

- [ ] **Step 5: Run the tests and checks to verify they pass**

```bash
timeout 300 npm --prefix web test -- src/pages/runners.test.tsx
timeout 300 npm --prefix web test
timeout 300 npm --prefix web run typecheck
timeout 300 npm --prefix web run lint
```
Expected: PASS (8 tests, and the full suite); typecheck and lint exit 0.

- [ ] **Step 6: Commit**

```bash
git add web/src/components/runners-table.tsx web/src/pages/runners.tsx web/src/pages/runners.test.tsx
git commit -m "feat(web): add the Runners page"
```

---

### Task 17: web: Dashboard page

**Files:**
- Modify (rewrite): `web/src/pages/dashboard.tsx`
- Test: `web/src/pages/dashboard.test.tsx`

**Interfaces:**
- Consumes: C4 types, `fixtures`, `authedRoutes`; C5 `api`, `mockApi`, `json`, `noContent`; C6 `keys`, `useStatus`, `useConfig`, `useMetrics`, `useEvents`; C7 `ago`, `clock`, `fmtMem`, `maxText`, `series`, `useNow`; C8 `StateBadge`, `Sparkline`; C10 `errorText`, `renderApp`; C12 `RunnersTable`.
- Produces: nothing other tasks read.

**Items:** 7

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 1 = 3

- [ ] **Step 1: Write the failing test `web/src/pages/dashboard.test.tsx`**

```tsx
import { screen, waitFor } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import { json, mockApi, noContent } from "@/test/api"
import { authedRoutes, fixtures } from "@/test/fixtures"
import { renderApp } from "@/test/render"

describe("Dashboard page", () => {
  it("shows the status chips", async () => {
    mockApi(authedRoutes())
    renderApp("/")
    expect(await screen.findByText("mode QUEUE")).toBeInTheDocument()
    expect(screen.getByText("runners 2/2")).toBeInTheDocument()
    expect(screen.getByText("api 4980")).toBeInTheDocument()
    expect(screen.getByText("disk 61%")).toBeInTheDocument()
    expect(screen.getByText("runner ↑ 2.338.0")).toBeInTheDocument()
  })

  it("shows the metric tiles", async () => {
    mockApi(authedRoutes())
    renderApp("/")
    expect(await screen.findByText("2 / 2")).toBeInTheDocument()
    expect(await screen.findByText("31%")).toBeInTheDocument()
    expect(screen.getByText("3.0G / 16.0G")).toBeInTheDocument()
    expect(screen.getByRole("img", { name: "running" })).toBeInTheDocument()
  })

  it("shows repos, runners and the activity feed", async () => {
    mockApi(authedRoutes())
    renderApp("/")
    expect(await screen.findByText("old-repo")).toBeInTheDocument()
    expect(screen.getByText("GitHub: not found")).toBeInTheDocument()
    expect(screen.getByText("paused")).toBeInTheDocument()
    expect(screen.getByText(/✔ #41 build/)).toBeInTheDocument()
    expect(screen.getByRole("link", { name: "aaaaaa" })).toBeInTheDocument()
    expect(await screen.findByText("runner aaaaaa started")).toBeInTheDocument()
  })

  it("pauses every repo", async () => {
    const { calls } = mockApi(authedRoutes({ "POST /api/pause-all": () => noContent() }))
    const { user } = renderApp("/")
    await user.click(await screen.findByRole("button", { name: "Pause all" }))
    await waitFor(() => expect(calls.some((c) => c.method === "POST" && c.path === "/api/pause-all")).toBe(true))
    expect((await screen.findAllByText("paused all repos (drain)")).length).toBeGreaterThan(0)
  })

  it("resumes when every live repo is paused", async () => {
    const repos = fixtures.status.repos.map((r) => ({ ...r, paused: true }))
    const { calls } = mockApi(
      authedRoutes({ "GET /api/status": { ...fixtures.status, repos }, "POST /api/resume-all": () => noContent() }),
    )
    const { user } = renderApp("/")
    await user.click(await screen.findByRole("button", { name: "Resume all" }))
    await waitFor(() => expect(calls.some((c) => c.path === "/api/resume-all")).toBe(true))
  })

  it("shows a metrics failure in the tiles", async () => {
    mockApi(authedRoutes({ "GET /api/metrics": () => json({ error: "boom" }, 500) }))
    renderApp("/")
    expect((await screen.findAllByText("✖ boom")).length).toBe(2)
  })

  it("shows the empty states", async () => {
    mockApi(authedRoutes({ "GET /api/status": { ...fixtures.status, repos: [], instances: [] }, "GET /api/events": [] }))
    renderApp("/")
    expect(await screen.findByText("no repos configured — add one on the Repositories page")).toBeInTheDocument()
    expect(screen.getByText("no runners — they start when jobs are queued")).toBeInTheDocument()
    expect(screen.getByText("no activity yet")).toBeInTheDocument()
  })

  it("waits for the daemon while it cannot be reached", async () => {
    mockApi(authedRoutes({ "GET /api/status": () => json({ error: "connection refused" }, 502) }))
    renderApp("/")
    expect(await screen.findByText("waiting for the daemon…")).toBeInTheDocument()
  })
})
```

- [ ] **Step 2: Run it to verify it fails**

Run: `timeout 300 npm --prefix web test -- src/pages/dashboard.test.tsx`
Expected: FAIL — `Unable to find an element with the text: mode QUEUE`.

- [ ] **Step 3: Rewrite `web/src/pages/dashboard.tsx`**

```tsx
import { useMutation, useQueryClient, type UseQueryResult } from "@tanstack/react-query"
import { Badge, type BadgeVariant } from "darkraise-ui/components/badge"
import { Button } from "darkraise-ui/components/button"
import { Card, CardContent, CardHeader, CardTitle } from "darkraise-ui/components/card"
import { toast } from "darkraise-ui/components/sonner"
import { Spinner } from "darkraise-ui/components/spinner"
import { Stat, StatLabel, StatValue } from "darkraise-ui/components/stat"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "darkraise-ui/components/table"
import { PageHeader } from "darkraise-ui/layout"
import { useEffect, useRef, type ReactNode } from "react"
import { api } from "@/api/client"
import { keys, useConfig, useEvents, useMetrics, useStatus } from "@/api/hooks"
import type { GhrEvent, Metrics, RepoStatus, Status } from "@/api/types"
import { RunnersTable } from "@/components/runners-table"
import { Sparkline } from "@/components/sparkline"
import { StateBadge } from "@/components/state-badge"
import { ago, clock, fmtMem, maxText, series } from "@/lib/format"
import { useNow } from "@/lib/use-now"
import { errorText } from "@/query"

const DAY = 86_400_000

function running(status: Status): number {
  return status.instances.filter((i) => i.state !== "cleaning").length
}

function capText(status: Status): string {
  return status.mode === "all" ? "∞" : String(status.global_max)
}

function allPaused(repos: RepoStatus[]): boolean {
  const live = repos.filter((r) => !r.removing)
  return live.length > 0 && live.every((r) => r.paused)
}

function diskVariant(pct: number, highWater: number): BadgeVariant {
  if (pct >= 95) return "red"
  return pct >= highWater ? "amber" : "secondary"
}

function repoState(r: RepoStatus): string {
  let state = "active"
  if (r.paused) state = "paused"
  if (r.removing) state = "removing"
  if (r.error) state = "error"
  return state
}

function StatusChips({ status, highWater, now }: { status: Status; highWater: number; now: number }) {
  const deadline = status.runner_update.deadline
  return (
    <div className="mb-4 flex flex-wrap gap-2">
      <Badge variant="outline">mode {status.mode.toUpperCase()}</Badge>
      <Badge variant="outline">
        runners {running(status)}/{capText(status)}
      </Badge>
      <Badge variant="outline">api {status.rate_remaining}</Badge>
      <Badge variant={diskVariant(status.disk_pct, highWater)}>disk {status.disk_pct}%</Badge>
      {status.degraded && <Badge variant="red">degraded: {status.degraded_reason}</Badge>}
      {deadline && (
        <Badge variant={Date.parse(deadline) - now <= 7 * DAY ? "red" : "amber"}>runner ↑ {status.runner_update.latest}</Badge>
      )}
    </div>
  )
}

function Tile({ label, value, children }: { label: string; value: string; children?: ReactNode }) {
  return (
    <Card>
      <CardContent className="p-4">
        <Stat>
          <StatLabel>{label}</StatLabel>
          <StatValue>{value}</StatValue>
        </Stat>
        {children}
      </CardContent>
    </Card>
  )
}

function StatTiles({ status, metrics }: { status: Status; metrics: UseQueryResult<Metrics> }) {
  const m = metrics.data
  const samples = m?.samples ?? []
  const failed = metrics.isError ? `✖ ${errorText(metrics.error)}` : undefined
  const queued = status.repos.reduce((n, r) => n + r.queued, 0)
  const cpu = m?.cpu !== undefined ? `${m.cpu.toFixed(0)}%` : "–"
  const mem = m?.mem_used !== undefined && m.mem_total !== undefined ? `${fmtMem(m.mem_used)} / ${fmtMem(m.mem_total)}` : "–"
  return (
    <div className="mb-4 grid gap-4 sm:grid-cols-2 xl:grid-cols-5">
      <Tile label="Running" value={`${running(status)} / ${capText(status)}`}>
        <Sparkline label="running" values={series(samples, (s) => s.live)} max={status.mode === "all" ? undefined : status.global_max} />
      </Tile>
      <Tile label="Queued jobs" value={String(queued)}>
        <Sparkline label="queued jobs" values={series(samples, (s) => s.queued)} />
      </Tile>
      <Tile label="CPU" value={failed ?? cpu}>
        <Sparkline label="cpu" values={series(samples, (s) => s.cpu)} max={100} />
      </Tile>
      <Tile label="Memory" value={failed ?? mem} />
      <Tile label="Disk" value={`${status.disk_pct}%`} />
    </div>
  )
}

function RepoTable({ repos, now }: { repos: RepoStatus[]; now: number }) {
  if (repos.length === 0) {
    return <p className="py-6 text-center text-sm text-muted-foreground">no repos configured — add one on the Repositories page</p>
  }
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>Repo</TableHead>
          <TableHead>State</TableHead>
          <TableHead>Run</TableHead>
          <TableHead>Queue</TableHead>
          <TableHead>Last job</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {repos.map((r) => (
          <TableRow key={r.name}>
            <TableCell>{r.name}</TableCell>
            <TableCell>
              <StateBadge state={repoState(r)} />
            </TableCell>
            <TableCell>
              {r.active}/{maxText(r.max)}
            </TableCell>
            <TableCell>{r.queued > 0 ? <span className="text-amber-600">⧗ {r.queued}</span> : "–"}</TableCell>
            <TableCell>
              {r.last_job
                ? `${r.last_job.conclusion === "success" ? "✔" : "✖"} #${r.last_job.run_number} ${r.last_job.job_name}  ${ago(now - Date.parse(r.last_job.finished_at))}`
                : "–"}
              {r.error && <span className="ml-2 text-destructive">{r.error}</span>}
            </TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  )
}

const eventIcon: Record<string, string> = { ok: "✔", warn: "⚠", error: "✖" }
const eventColour: Record<string, string> = { ok: "text-green-600", warn: "text-amber-600", error: "text-destructive" }

function ActivityFeed({ events }: { events: GhrEvent[] }) {
  const ref = useRef<HTMLDivElement>(null)
  useEffect(() => {
    const el = ref.current
    if (el) el.scrollTop = el.scrollHeight
  }, [events])
  if (events.length === 0) return <p className="py-6 text-center text-sm text-muted-foreground">no activity yet</p>
  return (
    <div ref={ref} className="max-h-80 overflow-auto font-mono text-xs">
      <ul className="flex flex-col gap-1">
        {events.map((e) => (
          <li key={e.seq} className="flex gap-2">
            <span className="text-muted-foreground">{clock(e.time)}</span>
            <span className={eventColour[e.level] ?? "text-primary"}>{eventIcon[e.level] ?? "▶"}</span>
            <span className="w-24 shrink-0 truncate">{e.repo ?? ""}</span>
            <span>{e.msg}</span>
          </li>
        ))}
      </ul>
    </div>
  )
}

export function DashboardPage() {
  const status = useStatus()
  const config = useConfig()
  const metrics = useMetrics()
  const events = useEvents(status.data?.epoch)
  const now = useNow()
  const queryClient = useQueryClient()
  const toggle = useMutation({
    mutationFn: (resume: boolean) => (resume ? api.resumeAll() : api.pauseAll()),
    onSuccess: (_data, resume) => {
      toast.success(resume ? "resumed all repos" : "paused all repos (drain)")
    },
    onSettled: () => queryClient.invalidateQueries({ queryKey: keys.status }),
  })

  const st = status.data
  if (!st) {
    return (
      <>
        <PageHeader title="Dashboard" />
        <Spinner label="waiting for the daemon…" />
      </>
    )
  }
  const paused = allPaused(st.repos)
  return (
    <>
      <PageHeader
        title="Dashboard"
        actions={
          <Button variant="secondary" disabled={status.isError || toggle.isPending} onClick={() => toggle.mutate(paused)}>
            {paused ? "Resume all" : "Pause all"}
          </Button>
        }
      />
      <StatusChips status={st} highWater={config.data?.disk_high_water ?? 80} now={now} />
      <StatTiles status={st} metrics={metrics} />
      <div className="mb-4 grid gap-4 lg:grid-cols-2">
        <Card>
          <CardHeader>
            <CardTitle>Repositories</CardTitle>
          </CardHeader>
          <CardContent>
            <RepoTable repos={st.repos} now={now} />
          </CardContent>
        </Card>
        <Card>
          <CardHeader>
            <CardTitle>Runners</CardTitle>
          </CardHeader>
          <CardContent>
            <RunnersTable status={st} actions={false} />
          </CardContent>
        </Card>
      </div>
      <Card>
        <CardHeader>
          <CardTitle>Activity</CardTitle>
        </CardHeader>
        <CardContent>
          <ActivityFeed events={events} />
        </CardContent>
      </Card>
    </>
  )
}
```

- [ ] **Step 4: Run the tests and checks to verify they pass**

```bash
timeout 300 npm --prefix web test -- src/pages/dashboard.test.tsx
timeout 300 npm --prefix web test
timeout 300 npm --prefix web run typecheck
timeout 300 npm --prefix web run lint
```
Expected: PASS (8 tests, and the full suite: `shell.test.tsx` and `app.test.tsx` still find the `Dashboard` heading); typecheck and lint exit 0.

- [ ] **Step 5: Commit**

```bash
git add web/src/pages/dashboard.tsx web/src/pages/dashboard.test.tsx
git commit -m "feat(web): add the Dashboard page"
```

---

### Task 18: web: runner detail page

**Files:**
- Create: `web/src/components/log-view.tsx`
- Modify (rewrite): `web/src/pages/runner-detail.tsx` (keeps `export type DetailTab`)
- Test: `web/src/pages/runner-detail.test.tsx`, `web/src/components/log-view.test.tsx`

**Interfaces:**
- Consumes: C4 types, `fixtures`, `authedRoutes`; C5 `mockApi`, `json`, `noContent`; C6 `useStatus`, `useSteps`, `useContainers`, `useLogTail`; C7 `dur`, `dateTimeSec`, `isZeroTime`, `useNow`; C8 `StateBadge`; C10 `errorText`, `renderApp`; C11 `Shell` (its reconnecting banner text); C12 `StopRunnerDialog`.
- Produces: nothing other tasks read.

**Items:** 4, 9

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 1 = 3

- [ ] **Step 1: Write the failing tests**

`web/src/components/log-view.test.tsx`:
```tsx
import { render } from "@testing-library/react"
import { afterEach, describe, expect, it } from "vitest"
import { LogView } from "./log-view"

let sets: number[] = []

afterEach(() => {
  sets = []
  Reflect.deleteProperty(HTMLElement.prototype, "scrollHeight")
  Reflect.deleteProperty(HTMLElement.prototype, "scrollTop")
})

function trackScroll() {
  Object.defineProperty(HTMLElement.prototype, "scrollHeight", { configurable: true, get: () => 500 })
  Object.defineProperty(HTMLElement.prototype, "scrollTop", {
    configurable: true,
    get: () => 0,
    set: (v: number) => {
      sets.push(v)
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
})
```

`web/src/pages/runner-detail.test.tsx`:
```tsx
import { screen, waitFor, within } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import { json, mockApi, noContent } from "@/test/api"
import { authedRoutes, fixtures } from "@/test/fixtures"
import { renderApp } from "@/test/render"

const detailRoutes = (over: Record<string, unknown> = {}) =>
  authedRoutes({
    "GET /api/runners/aaaaaa/steps": fixtures.steps,
    "GET /api/runners/aaaaaa/containers": fixtures.containers,
    "GET /api/runners/aaaaaa/log": fixtures.log,
    ...over,
  })

describe("runner detail page", () => {
  it("shows the runner and its steps", async () => {
    mockApi(detailRoutes())
    renderApp("/runners/aaaaaa")
    expect(await screen.findByRole("heading", { name: "aaaaaa" })).toBeInTheDocument()
    expect(await screen.findByText("Run tests")).toBeInTheDocument()
    expect(screen.getByText(/test #42/)).toBeInTheDocument()
    expect(screen.getByRole("link", { name: "Open run" })).toHaveAttribute(
      "href",
      "https://github.com/darkraise/darkmem/actions/runs/102/job/2",
    )
  })

  it("follows the log on the Log tab", async () => {
    mockApi(detailRoutes())
    renderApp("/runners/aaaaaa?tab=log")
    expect(await screen.findByText(/Listening for Jobs/)).toBeInTheDocument()
    expect(screen.getByRole("switch", { name: "Follow" })).toHaveAttribute("aria-checked", "true")
  })

  it("lists the containers", async () => {
    mockApi(detailRoutes())
    renderApp("/runners/aaaaaa?tab=containers")
    expect(await screen.findByText("ghr-aaaaaa-db-1")).toBeInTheDocument()
  })

  it("puts the chosen tab in the URL", async () => {
    mockApi(detailRoutes())
    const { user, router } = renderApp("/runners/aaaaaa")
    await user.click(await screen.findByRole("tab", { name: "Log" }))
    await waitFor(() => expect(router.state.location.search).toEqual({ tab: "log" }))
  })

  it("stops the runner only after confirmation", async () => {
    const { calls } = mockApi(detailRoutes({ "DELETE /api/runners/aaaaaa": () => noContent() }))
    const { user } = renderApp("/runners/aaaaaa")
    await user.click(await screen.findByRole("button", { name: "Stop runner" }))
    await user.click(screen.getByRole("button", { name: "Cancel" }))
    expect(calls.some((c) => c.method === "DELETE")).toBe(false)
    await user.click(screen.getByRole("button", { name: "Stop runner" }))
    await user.click(within(await screen.findByRole("alertdialog")).getByRole("button", { name: "Stop" }))
    await waitFor(() => expect(calls.some((c) => c.method === "DELETE" && c.path === "/api/runners/aaaaaa")).toBe(true))
  })

  it("keeps the last snapshot after the runner ends", async () => {
    let gone = false
    mockApi(
      detailRoutes({
        "GET /api/status": () =>
          gone ? { ...fixtures.status, instances: fixtures.status.instances.filter((i) => i.id !== "aaaaaa") } : fixtures.status,
      }),
    )
    renderApp("/runners/aaaaaa")
    expect(await screen.findByText("Run tests")).toBeInTheDocument()
    gone = true
    expect(await screen.findByText("This runner has finished", {}, { timeout: 3000 })).toBeInTheDocument()
    expect(screen.getByText(/test #42/)).toBeInTheDocument()
    expect(screen.getByText("Run tests")).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Stop runner" })).toBeDisabled()
  })

  it("keeps the page up while the daemon cannot be reached", async () => {
    mockApi(detailRoutes({ "GET /api/status": () => json({ error: "connection refused" }, 502) }))
    renderApp("/runners/aaaaaa")
    expect(await screen.findByRole("heading", { name: "aaaaaa" })).toBeInTheDocument()
    expect(await screen.findByText("daemon unreachable: connection refused — retrying")).toBeInTheDocument()
    expect(screen.queryByText("This runner has finished")).toBeNull()
    expect(screen.getByRole("button", { name: "Stop runner" })).toBeDisabled()
  })

  it("shows the steps' empty state", async () => {
    mockApi(detailRoutes({ "GET /api/runners/aaaaaa/steps": [] }))
    renderApp("/runners/aaaaaa")
    expect(await screen.findByText("no steps reported yet")).toBeInTheDocument()
  })
})
```

- [ ] **Step 2: Run them to verify they fail**

Run: `timeout 300 npm --prefix web test -- src/pages/runner-detail.test.tsx src/components/log-view.test.tsx`
Expected: FAIL — `Failed to resolve import "./log-view"` and `Unable to find role="heading" and name "aaaaaa"`.

- [ ] **Step 3: Write `web/src/components/log-view.tsx`**

```tsx
import { useEffect, useRef } from "react"

export function LogView({ text, follow }: { text: string; follow: boolean }) {
  const ref = useRef<HTMLPreElement>(null)
  useEffect(() => {
    const el = ref.current
    if (follow && el) el.scrollTop = el.scrollHeight
  }, [text, follow])
  return (
    <pre ref={ref} className="h-[60vh] overflow-auto whitespace-pre-wrap rounded-md border bg-muted/40 p-3 font-mono text-xs">
      {text || "no log output yet"}
    </pre>
  )
}
```

- [ ] **Step 4: Rewrite `web/src/pages/runner-detail.tsx`**

```tsx
import type { UseQueryResult } from "@tanstack/react-query"
import { useNavigate, useParams, useSearch } from "@tanstack/react-router"
import { Alert, AlertDescription, AlertTitle } from "darkraise-ui/components/alert"
import { Button } from "darkraise-ui/components/button"
import { Label } from "darkraise-ui/components/label"
import { Spinner } from "darkraise-ui/components/spinner"
import { Switch } from "darkraise-ui/components/switch"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "darkraise-ui/components/table"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "darkraise-ui/components/tabs"
import { PageHeader } from "darkraise-ui/layout"
import { useEffect, useState } from "react"
import { useContainers, useLogTail, useStatus, useSteps } from "@/api/hooks"
import type { Container, InstanceStatus, Step } from "@/api/types"
import { LogView } from "@/components/log-view"
import { StopRunnerDialog } from "@/components/runners-table"
import { StateBadge } from "@/components/state-badge"
import { dateTimeSec, dur, isZeroTime } from "@/lib/format"
import { useNow } from "@/lib/use-now"
import { errorText } from "@/query"

export type DetailTab = "steps" | "log" | "containers"

function stepIcon(s: Step) {
  if (s.status === "in_progress") return <Spinner size="sm" />
  if (s.conclusion === "success") return <span className="text-green-600">✔</span>
  if (s.conclusion === "failure") return <span className="text-destructive">✖</span>
  if (s.conclusion === "skipped") return <span>–</span>
  return <span className="text-muted-foreground">○</span>
}

function StepList({ steps }: { steps: UseQueryResult<Step[]> }) {
  return (
    <>
      {steps.isError && <p className="mb-2 text-sm text-destructive">{errorText(steps.error)}</p>}
      {steps.data && steps.data.length > 0 ? (
        <ul className="flex flex-col gap-1">
          {steps.data.map((s) => (
            <li key={s.number} className="flex items-center gap-2">
              {stepIcon(s)}
              <span>{s.name}</span>
            </li>
          ))}
        </ul>
      ) : (
        <p className="text-sm text-muted-foreground">no steps reported yet</p>
      )}
    </>
  )
}

function ContainerTable({ containers }: { containers: UseQueryResult<Container[]> }) {
  return (
    <>
      {containers.isError && <p className="mb-2 text-sm text-destructive">{errorText(containers.error)}</p>}
      {containers.data && containers.data.length > 0 ? (
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
                <TableCell>{c.state}</TableCell>
                <TableCell>{c.name}</TableCell>
                <TableCell>{c.image}</TableCell>
                <TableCell className="text-muted-foreground">{c.project}</TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      ) : (
        <p className="text-sm text-muted-foreground">none in this runner's compose projects</p>
      )}
    </>
  )
}

export function RunnerDetailPage() {
  const { id = "" } = useParams({ strict: false }) as { id?: string }
  const { tab = "steps" } = useSearch({ strict: false }) as { tab?: DetailTab }
  const navigate = useNavigate()
  const status = useStatus()
  const now = useNow()
  const [snapshot, setSnapshot] = useState<InstanceStatus | undefined>(undefined)
  const [finishedAt, setFinishedAt] = useState<number | undefined>(undefined)
  const [follow, setFollow] = useState(true)
  const [stopping, setStopping] = useState(false)

  const current = status.data?.instances.find((i) => i.id === id)
  const live = current !== undefined
  const finished = status.data !== undefined && !live

  // The detail page outlives the runner: it keeps the last instance it saw,
  // and stops polling steps and containers once the runner leaves /status.
  useEffect(() => {
    if (current) {
      setSnapshot(current)
      setFinishedAt(undefined)
    } else if (status.data && finishedAt === undefined) {
      setFinishedAt(Date.now())
    }
  }, [current, status.data, finishedAt])

  const steps = useSteps(id, live)
  const containers = useContainers(id, live)
  const log = useLogTail(id, tab === "log")

  const inst = current ?? snapshot
  const job = inst?.job
  const start = job && !isZeroTime(job.started_at) ? job.started_at : inst?.since
  const end = finished ? (finishedAt ?? now) : now

  return (
    <>
      <PageHeader
        breadcrumbs={[{ label: "Runners", href: "/runners" }, { label: id }]}
        title={id}
        actions={
          <div className="flex gap-2">
            {job?.html_url && (
              <Button variant="outline" asChild>
                <a href={job.html_url} target="_blank" rel="noreferrer">
                  Open run
                </a>
              </Button>
            )}
            <Button variant="destructive" disabled={!live || status.isError} onClick={() => setStopping(true)}>
              Stop runner
            </Button>
          </div>
        }
      />
      {finished && (
        <Alert variant="warning" className="mb-4">
          <AlertTitle>This runner has finished</AlertTitle>
          <AlertDescription>
            <Button variant="link" className="px-0" onClick={() => void navigate({ to: "/runners" })}>
              Back to runners
            </Button>
          </AlertDescription>
        </Alert>
      )}
      {inst && (
        <p className="mb-4 flex flex-wrap items-center gap-2 text-sm">
          <StateBadge state={inst.state} />
          <span>· {inst.repo}</span>
          {job && <span>· {`${job.name} #${job.run_number}`}</span>}
          {start && <span>· {dur(end - Date.parse(start))}</span>}
          {start && <span>· started {dateTimeSec(start)}</span>}
        </p>
      )}
      <Tabs value={tab} onValueChange={(t) => void navigate({ to: "/runners/$id", params: { id }, search: { tab: t as DetailTab } })}>
        <TabsList>
          <TabsTrigger value="steps">Steps</TabsTrigger>
          <TabsTrigger value="log">Log</TabsTrigger>
          <TabsTrigger value="containers">Containers</TabsTrigger>
        </TabsList>
        <TabsContent value="steps">
          <StepList steps={steps} />
        </TabsContent>
        <TabsContent value="log">
          <div className="mb-2 flex items-center gap-2">
            <Switch id="follow" checked={follow} onCheckedChange={setFollow} />
            <Label htmlFor="follow">Follow</Label>
          </div>
          {log.isError && <p className="mb-2 text-sm text-destructive">{errorText(log.error)}</p>}
          <LogView text={log.data?.text ?? ""} follow={follow} />
        </TabsContent>
        <TabsContent value="containers">
          <ContainerTable containers={containers} />
        </TabsContent>
      </Tabs>
      <StopRunnerDialog instance={stopping && inst ? inst : null} onClose={() => setStopping(false)} />
    </>
  )
}
```

- [ ] **Step 5: Point the router test at the real detail heading**

The page's heading is now the runner ID, not the stub's `Runner`. In `web/src/router.test.tsx`, in the `it.each` table, replace the row
```tsx
    ["/runners/aaaaaa", "Runner"],
```
with
```tsx
    ["/runners/aaaaaa", "aaaaaa"],
```
The router tests mock only `/auth/state`; the page takes its heading from the URL, and with no status it polls neither steps nor containers.

- [ ] **Step 6: Run the tests and checks to verify they pass**

```bash
timeout 300 npm --prefix web test -- src/pages/runner-detail.test.tsx src/components/log-view.test.tsx
timeout 300 npm --prefix web test
timeout 300 npm --prefix web run typecheck
timeout 300 npm --prefix web run lint
```
Expected: PASS (10 tests, and the full suite); typecheck and lint exit 0.

- [ ] **Step 7: Commit**

```bash
git add web/src/components/log-view.tsx web/src/components/log-view.test.tsx web/src/pages/runner-detail.tsx web/src/pages/runner-detail.test.tsx web/src/router.test.tsx
git commit -m "feat(web): add the runner detail page"
```

---

### Task 19: web: History page

**Files:**
- Modify (rewrite): `web/src/pages/history.tsx`
- Test: `web/src/pages/history.test.tsx`

**Interfaces:**
- Consumes: C4 `fixtures`, `authedRoutes`; C5 `mockApi`, `json`; C6 `useHistory`, `useConfig`; C7 `dateTime`, `dur`; C8 `StateBadge`; C10 `errorText`, `renderApp`.
- Produces: nothing other tasks read.

**Items:** 10

**Implementer:** dr-superpowers:impl-sonnet-low
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 0 = 2

- [ ] **Step 1: Write the failing test `web/src/pages/history.test.tsx`**

```tsx
import { screen, waitFor } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import { json, mockApi } from "@/test/api"
import { authedRoutes, fixtures } from "@/test/fixtures"
import { renderApp } from "@/test/render"

describe("History page", () => {
  it("lists finished jobs, newest first as served", async () => {
    const { calls } = mockApi(authedRoutes({ "GET /api/history": fixtures.history }))
    renderApp("/history")
    expect(await screen.findByText("build")).toBeInTheDocument()
    expect(screen.getByText("deploy")).toBeInTheDocument()
    expect(screen.getByText("#41")).toBeInTheDocument()
    expect(screen.getByText("success")).toBeInTheDocument()
    expect(screen.getByText("failure")).toBeInTheDocument()
    expect(screen.getAllByRole("link", { name: "Open run" })).toHaveLength(1)
    expect(calls.find((c) => c.path === "/api/history")?.search).toBe("?repo=&conclusion=&limit=200")
  })

  it("filters by repo", async () => {
    const { calls } = mockApi(authedRoutes({ "GET /api/history": fixtures.history }))
    const { user } = renderApp("/history")
    await screen.findByText("build")
    await user.click(screen.getByRole("combobox", { name: "Repo" }))
    await user.click(await screen.findByRole("option", { name: "darkmem" }))
    await waitFor(() => expect(calls.some((c) => c.search === "?repo=darkmem&conclusion=&limit=200")).toBe(true))
  })

  it("filters by result", async () => {
    const { calls } = mockApi(authedRoutes({ "GET /api/history": fixtures.history }))
    const { user } = renderApp("/history")
    await screen.findByText("build")
    await user.click(screen.getByRole("combobox", { name: "Result" }))
    await user.click(await screen.findByRole("option", { name: "failure" }))
    await waitFor(() => expect(calls.some((c) => c.search === "?repo=&conclusion=failure&limit=200")).toBe(true))
  })

  it("says when nothing has finished", async () => {
    mockApi(authedRoutes({ "GET /api/history": [] }))
    renderApp("/history")
    expect(await screen.findByText("no finished jobs yet")).toBeInTheDocument()
  })

  it("shows a failed read", async () => {
    mockApi(authedRoutes({ "GET /api/history": () => json({ error: "history file unreadable" }, 500) }))
    renderApp("/history")
    expect(await screen.findByText("history file unreadable")).toBeInTheDocument()
  })
})
```

- [ ] **Step 2: Run it to verify it fails**

Run: `timeout 300 npm --prefix web test -- src/pages/history.test.tsx`
Expected: FAIL — `Unable to find an element with the text: build`.

- [ ] **Step 3: Rewrite `web/src/pages/history.tsx`**

```tsx
import { Card, CardContent } from "darkraise-ui/components/card"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "darkraise-ui/components/select"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "darkraise-ui/components/table"
import { PageHeader } from "darkraise-ui/layout"
import { useState } from "react"
import { useConfig, useHistory } from "@/api/hooks"
import { StateBadge } from "@/components/state-badge"
import { dateTime, dur } from "@/lib/format"
import { errorText } from "@/query"

const ALL = "__all__"
const RESULTS = ["success", "failure", "cancelled"]

export function HistoryPage() {
  const [repo, setRepo] = useState("")
  const [conclusion, setConclusion] = useState("")
  const config = useConfig()
  const history = useHistory(repo, conclusion)

  const names = (config.data?.repos ?? []).map((r) => r.name)
  const repos = repo && !names.includes(repo) ? [...names, repo] : names
  const rows = history.data ?? []
  const longest = Math.max(1, ...rows.map((h) => Date.parse(h.finished_at) - Date.parse(h.started_at)))

  return (
    <>
      <PageHeader title="History" />
      <div className="mb-4 flex flex-wrap gap-4">
        <Select value={repo || ALL} onValueChange={(v) => setRepo(v === ALL ? "" : v)}>
          <SelectTrigger aria-label="Repo" className="w-48">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value={ALL}>all repos</SelectItem>
            {repos.map((name) => (
              <SelectItem key={name} value={name}>
                {name}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Select value={conclusion || ALL} onValueChange={(v) => setConclusion(v === ALL ? "" : v)}>
          <SelectTrigger aria-label="Result" className="w-40">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value={ALL}>all results</SelectItem>
            {RESULTS.map((r) => (
              <SelectItem key={r} value={r}>
                {r}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>
      <Card>
        <CardContent className="p-4">
          {history.isError && <p className="mb-2 text-sm text-destructive">{errorText(history.error)}</p>}
          {rows.length === 0 && !history.isError ? (
            <p className="py-6 text-center text-sm text-muted-foreground">no finished jobs yet</p>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Finished</TableHead>
                  <TableHead>Repo</TableHead>
                  <TableHead>Run</TableHead>
                  <TableHead>Job</TableHead>
                  <TableHead>Result</TableHead>
                  <TableHead>Duration</TableHead>
                  <TableHead />
                </TableRow>
              </TableHeader>
              <TableBody>
                {rows.map((h) => {
                  const took = Date.parse(h.finished_at) - Date.parse(h.started_at)
                  return (
                    <TableRow key={h.id}>
                      <TableCell>{dateTime(h.finished_at)}</TableCell>
                      <TableCell>{h.repo}</TableCell>
                      <TableCell>#{h.run_number}</TableCell>
                      <TableCell>{h.job_name}</TableCell>
                      <TableCell>
                        <StateBadge state={h.conclusion} />
                      </TableCell>
                      <TableCell>
                        <div className="flex items-center gap-2">
                          <span className="w-14">{dur(took)}</span>
                          <div className="h-1.5 w-24 rounded bg-muted">
                            <div className="h-1.5 rounded bg-primary" style={{ width: `${Math.round((took / longest) * 100)}%` }} />
                          </div>
                        </div>
                      </TableCell>
                      <TableCell className="text-right">
                        {h.html_url && (
                          <a href={h.html_url} target="_blank" rel="noreferrer" className="text-sm hover:underline">
                            Open run
                          </a>
                        )}
                      </TableCell>
                    </TableRow>
                  )
                })}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>
    </>
  )
}
```

- [ ] **Step 4: Run the tests and checks to verify they pass**

```bash
timeout 300 npm --prefix web test -- src/pages/history.test.tsx
timeout 300 npm --prefix web test
timeout 300 npm --prefix web run typecheck
timeout 300 npm --prefix web run lint
timeout 600 npm --prefix web run build
git status --porcelain web/dist
timeout 400 go test ./...
```
Expected: PASS (5 tests, and the full suite); typecheck and lint exit 0; the build succeeds, `web/dist` shows no changes, and `go test ./...` is `ok` (the fixture test included).

- [ ] **Step 5: Commit**

```bash
git add web/src/pages/history.tsx web/src/pages/history.test.tsx
git commit -m "feat(web): add the History page"
```
