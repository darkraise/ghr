# ghr Forge UI Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: the skill the **Execution:** line names — dr-superpowers:subagent-driven-development for `subagent`, dr-superpowers:executing-plans for `inline`. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** ghr's web UI takes the Forge look (GitHub's palette, Mona Sans, boxes, status pills), a top bar with tabs in place of the sidebar, and GitHub-style run lists, run pages, repository rows and one settings area.

**Architecture:** Frontend only, in `web/`. ghr keeps overriding darkraise-ui's tokens from `web/src/styles/ghr-theme.css`; a foundation (fonts, palette, mode, pill, box, shell, shared row and side layout, section routes) lands first, then each page is rebuilt on those pieces. Two new routes carry a `$section` parameter so the route table stays fixed while the pages change.

**Tech Stack:** React 19, TypeScript 5.7, Tailwind 4, darkraise-ui 6.9, TanStack Router and Query, lucide-react, Vitest with Testing Library under jsdom.

**Spec:** docs/superpowers/specs/2026-10-10-ghr-forge-ui-design.md

**Execution:** subagent — `claude --model sonnet --effort high` — the plan runs parallel waves.

**Plan review:** 2026-10-11 — dr-superpowers:judge-opus — executability 14 / coherence 17 / coverage 16 / assumptions 13 (round 3)

**Parallelism:** waves — 1 | 2 | 3 | 4-6 | 7-9 | 10-12 | 13-14 | 15-16 | 17 | 18 | 19 | 20

**Integration check:** `cd web && timeout 600 npm test && timeout 300 npm run typecheck && timeout 300 npm run lint && timeout 600 npm run build`

**Worktree setup:** `cd web && timeout 600 npm ci`

## Global Constraints

- Work on the branch `forge-ui`, never on `master`. Do not push `master`: CI turns every push to `master` into a release. Do not commit `.superpowers/`.
- Frontend only. No file outside `web/` and `docs/` changes. No daemon, API or CLI change; if a page needs data it does not load today, stop and report.
- Web checks, from `web/`: `timeout 600 npm test`, `timeout 300 npm run typecheck`, `timeout 300 npm run lint` (eslint, `--max-warnings 0`). Run one test file with `timeout 300 npx vitest run <path>`.
- Never run `go test ./...` or `go vet ./...`: they walk `web/node_modules`. Go checks use `./internal/... ./cmd/... ./web/`.
- Tests run on fixtures under jsdom: no ports, databases or fixed paths. Use `fixtures`, `authedRoutes`, `renderApp` and `renderRoutes` from `web/src/test/`.
- No component reads a raw colour. Use the kit's Tailwind token classes (`bg-card`, `bg-background`, `bg-muted`, `border-border`, `text-foreground`, `text-muted-foreground`, `text-primary`, `text-success`, `text-destructive`, `text-warning`) and the new `running` colour (Contracts).
- Colour roles: succeeded is `success`, failed is `destructive`, running is `running`, waiting and queued are `warning`, links and primary actions are `primary`. Running is never `primary`.
- Shape: `rounded-md` (6px) on boxes, buttons and inputs; `rounded-full` on status pills, count badges and inline labels (a branch, a repository label, the "ghr" mark). 1px `border-border`. No drop shadows; the one allowed `shadow-[…]` is the inset 2px marker `shadow-[inset_2px_0_0_hsl(var(--primary))]` on a current list entry. No gradients, no backdrop blur (`web/src/anti-slop.test.ts` fails on them). Do not import the kit's `Badge`.
- Icons are lucide-react at `size={15}` (`size={13}` inside a status pill and a `RunValue`), with `aria-hidden="true"` unless the icon carries the meaning, in which case `role="img"` and an `aria-label`.
- Mono (`font-mono`) for names, IDs, run numbers, branches, durations and times. Weights 400 and 600 only (`font-semibold`), no `font-medium` in new code.
- A file that exports a React component exports nothing else (`react-refresh/only-export-components` is an error under `--max-warnings 0`); constants and helpers live in `web/src/lib/`.
- The kit's `PageHeader` takes `title` and `description` as strings. A page's status pill goes first in the header's `actions`.
- Wording, validation, dialogs, toasts, polling and URL parameters do not change unless a task says so.
- Not built: search, re-running jobs, log timestamp or wrap toggles.
- English only. Comments only where the why is not obvious. Commits: `<type>(<scope>): <subject>`, subject at most 50 characters, imperative, no period.

## Contracts

**Tokens and classes (Task 2)**

- CSS custom properties on both mode blocks of `web/src/styles/ghr-theme.css`: `--running`, `--top-bar`, `--tab-line`, as HSL triplets.
- Tailwind colour `running`: `text-running`, `bg-running`, `bg-running/15`, `fill-running`, `stroke-running`, `border-running`.
- `web/src/lib/palette.ts` exports `PALETTE: Record<Role, { light: string; dark: string }>` with roles `ground`, `surface`, `topBar`, `sunk`, `line`, `text`, `muted`, `accent`, `onAccent`, `tabLine`, `succeeded`, `failed`, `running`, `waiting`; `PILL_ALPHA = 0.15`; `contrast(a: string, b: string): number`; `over(fg: string, bg: string, alpha: number): string`; `hslTriplet(hex: string): string`.

**Status (Task 4)**

- `web/src/lib/status.ts`: `type Tone = "accent" | "run" | "warn" | "bad" | "ok" | "muted"`; `stateTone(state: string): Tone` maps `busy`, `running`, `active`, `in_progress` to `run`; `stateWord(state: string): string` gives the one word for a state (`busy` and `in_progress` are "Running", `queued` is "Waiting", `success` is "Succeeded", `failure` is "Failed", anything else its own name capitalised).
- `web/src/lib/state-icon.ts`: `stateIcon(state: string): LucideIcon`, `TONE_TEXT: Record<Tone, string>`, `TONE_FILL: Record<Tone, string>`.
- `web/src/components/page/status-pill.tsx`: `StatusPill({ state, label }: { state: string; label?: string })`. The word is `label`, or `stateWord(state)`. `ResultIcon` and `RunStatusIcon` keep their props and draw from the same mapping.
- `web/src/components/page/state-text.tsx` re-exports `StatusPill` as `StateText` until Task 20 deletes it.

**Box (Task 5)**

- `Section({ id?, title, aside?, children, className?, flush? })` in `web/src/components/page/section.tsx`. `flush` removes the body padding, for bodies that are a list or table.

**Rows and side layout (Task 8)**

- `web/src/components/page/run-row.tsx`:
  - `RunRow({ icon, title, facts?, values?, actions?, wide }: { icon: ReactNode; title: ReactNode; facts?: ReactNode; values?: ReactNode; actions?: ReactNode; wide: boolean })` renders one `<tr>`: an icon cell, a main cell (title, facts, and `values` under them when `wide` is false), a right-aligned values cell when `wide`, and an actions cell when `actions` is given.
  - `RunValue({ kind, children }: { kind: "time" | "duration"; children: ReactNode })`.
  - `RunGroup({ label, cols }: { label: string; cols: number })` renders the group heading `<tr>` holding a `<th scope="rowgroup">`.
- `web/src/lib/run-cols.ts`: `runCols(wide: boolean, actions: boolean): number`.
- `web/src/components/page/side-layout.tsx`: `SideLayout({ label, nav, children })` and `SideGroup({ children })`.
- `web/src/lib/side-link.ts`: the class string `SIDE_LINK`, for links that carry `aria-current="page"` when current.

**Routes (Task 9)**

- `/settings/$section` (route id `/app/settings/$section`) and `/repositories/$name/$section` (route id `/app/repositories/$name/$section`). Pages read parameters with `useParams({ strict: false })`.
- Settings sections: `token`, `runner`, `account`, `toolchains`, `storage`; no section means General. Repository sections: `settings`, `labels`, `registrations`; no section means Overview.

**Update banner (Task 6)**

- `UpdateCard({ update, offline })` keeps its name, props, states and wording; it lays out as one horizontal banner.

**Shell (Task 7)**

- The app shell is ghr's own markup in `web/src/components/shell.tsx`: a `banner` landmark, a navigation named "Pages" holding the six tabs, and `main`.
- `web/src/styles/ghr-theme.css` has a block `:root[data-mode="light"] .ghr-log-panel { … }` that rebinds the page tokens to the dark palette; an element with the class `ghr-log-panel` is dark in both modes.
- `CapacitySegments({ status, size? })` has no `tone` prop; `CapacityBar` is gone.

## Assumptions (evidence)

- The kit's `TopNavLayout` is not used. It always renders a search box and a user menu, renders no item badge, puts the tabs in the same row as the brand, and hides them below 768px: reported by the round-1 plan review from `web/node_modules/darkraise-ui/dist/chunk-4S4GXBTH.js` (the file that defines `dr-top-nav-layout`), 2026-10-10; not re-read by the planner. Task 7 builds the shell from plain markup, so the plan does not depend on it.
- The kit layouts give `main` the id `main-content` and `web/src/a11y.test.tsx:10-11` looks the page up by it (round-2 plan review, 2026-10-11); Task 7 keeps the id.
- Decision on the borderline review score (assumptions 13 in round 3): proceed. The two open assumptions are the kit's font weights (below) and the `TopNavLayout` report (above); neither is consumed by a task.
- `@fontsource-variable/mona-sans` 5.3.0 and `@fontsource/jetbrains-mono` 5.3.0 are OFL-1.1: `npm view`, 2026-10-10. The font family names and CSS entry files are unverified — Task 1 verifies them.
- Palette contrast: every text and status colour is at least 4.57:1 on `surface` and `ground` in its mode (computed 2026-10-10 from the hex values; Task 2's test asserts it).
- A pill's coloured word cannot reach 4.5:1 on its own tint (light Running: about 4.3:1), so the word uses the text colour; spec §1.4 was amended for this on 2026-10-10.
- `theme.config.ts` accepts `mode: "system"`: `ModeControl` already calls `setMode("system")` (`web/src/components/mode-control.tsx`). Unverified for `defaults` — Task 3 verifies it with the typecheck.
- The kit's `PageHeader` takes `title: string` and `description?: string`: `web/node_modules/darkraise-ui/dist/layout/types.d.ts:56-62`, read 2026-10-10. Spec §5 and §6.4 were amended to put the status pill in the header's actions.
- Kit components keep the kit's own font weights; only ghr's markup is held to 400 and 600. Accepted without checking whether the kit has a weight token.
- The `--surface-sidebar` and `--sidebar-*` token overrides stay in both mode blocks although no sidebar remains: `generateTokens` still writes those keys and `web/src/styles/theme.test.ts:70-84` requires an override for each.
- `web/src/anti-slop.test.ts` forbids gradients, backdrop blur, `bg-clip-text` and the kit `Badge`; it has no rule on pills or inset markers (read 2026-10-10).
- The run page's summary is already a four-fact `dl` and Actions already renders title-plus-facts rows (`web/src/pages/run.tsx:154-190`, `web/src/pages/actions.tsx:32-75`, read 2026-10-10), so Tasks 12 and 14 restyle more than they restructure.
- No task carries an `**Executor:**` line although the Codex lane gate passed: the owner answered "none" to the lane question on 2026-10-10, so the plan is Claude-only.
- Test helpers named in the tasks exist as written: `renderApp` (`web/src/test/render.tsx`), `renderRoutes` (`web/src/test/routes.tsx`), `mockApi` (`web/src/test/api.ts`), `authedRoutes`, `runRoutes`, `fixtures` (`web/src/test/fixtures.ts`), `setViewport` (`web/src/test/media.ts`), and the per-file helpers `draw` and `inDays` (`update-card.test.tsx`), `draw` and `link` (`runner-list.test.tsx`), `renderFrame` (`run-frame.test.tsx`), `show` (`run-graph.test.tsx`), `renderViewer` (`log-viewer.test.tsx`), `routes` (`repository.test.tsx`, `settings.test.tsx`), `region` (`actions.test.tsx`, `dashboard.test.tsx`); read 2026-10-10.

## Task index

1. Fonts
2. Palette and contrast test
3. System colour mode by default
4. Status pill and tones
5. Box section
6. Update banner
7. Top-bar shell
8. Run row and side layout
9. Section routes
10. Dashboard
11. Runners
12. Actions
13. History
14. Run page
15. Job log panel
16. Repositories list
17. Repository page sections
18. Settings area
19. Redirects and links
20. Cleanup and release checks

---

### Task 1: Fonts

**Files:**
- Modify: `web/package.json`, `web/package-lock.json`
- Modify: `web/src/main.tsx:1-6`
- Modify: `web/src/styles/ghr-theme.css` (the first `@theme inline` block, and every rule naming `"IBM Plex Mono"`)

**Interfaces:**
- Consumes: nothing.
- Produces: `font-sans` is Mona Sans and `font-mono` is JetBrains Mono everywhere.

**Items:** 2

**Depends on:** none

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 1 - coupling 0 - risk 1 = 3

- [ ] **Step 1: Swap the packages**

```bash
cd web && timeout 300 npm uninstall @fontsource/ibm-plex-sans @fontsource/ibm-plex-mono \
  && timeout 300 npm install @fontsource-variable/mona-sans@^5.3.0 @fontsource/jetbrains-mono@^5.3.0
```

- [ ] **Step 2: Confirm the entry files and family names**

```bash
cd web && ls node_modules/@fontsource-variable/mona-sans/*.css | head; ls node_modules/@fontsource/jetbrains-mono/latin-400.css node_modules/@fontsource/jetbrains-mono/latin-600.css
rg -o "font-family: '[^']+'" node_modules/@fontsource-variable/mona-sans/index.css node_modules/@fontsource/jetbrains-mono/latin-400.css | sort -u
```

Expected: an `index.css` for Mona Sans, both JetBrains Mono files, and the family names `Mona Sans Variable` and `JetBrains Mono`. If a name differs, use the printed one in Step 3 and Step 4.

- [ ] **Step 3: Replace the imports in `web/src/main.tsx`**

Replace the six `@fontsource/ibm-plex-*` lines with:

```ts
import "@fontsource-variable/mona-sans/index.css"
import "@fontsource/jetbrains-mono/latin-400.css"
import "@fontsource/jetbrains-mono/latin-600.css"
```

- [ ] **Step 4: Point the theme at the new families**

In `web/src/styles/ghr-theme.css`, replace the body of the first `@theme inline` block with:

```css
  --font-sans: "Mona Sans Variable", ui-sans-serif, system-ui, -apple-system, "Segoe UI", sans-serif;
  --font-heading: "Mona Sans Variable", ui-sans-serif, system-ui, -apple-system, "Segoe UI", sans-serif;
  --font-mono: "JetBrains Mono", ui-monospace, SFMono-Regular, Menlo, monospace;
```

Then replace every remaining `"IBM Plex Mono", ui-monospace, monospace` in the file with `var(--font-mono)`.

- [ ] **Step 5: Verify**

Run: `cd web && rg -n "IBM Plex|ibm-plex" src public package.json; timeout 600 npm test && timeout 300 npm run typecheck && timeout 600 npm run build`
Expected: the `rg` prints nothing; tests, typecheck and build pass.

- [ ] **Step 6: Commit**

```bash
git add web/package.json web/package-lock.json web/src/main.tsx web/src/styles/ghr-theme.css
git commit -m "feat(web): use Mona Sans and JetBrains Mono"
```

### Task 2: Palette and contrast test

**Files:**
- Create: `web/src/lib/palette.ts`
- Test: `web/src/lib/palette.test.ts`
- Modify: `web/src/styles/ghr-theme.css` (the `:root[data-mode="dark"]` and `:root[data-mode="light"]` blocks; one new `@theme inline` block)

**Interfaces:**
- Consumes: nothing.
- Produces: Contracts, "Tokens and classes".

**Items:** 1, 2

**Depends on:** Task 1

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 1 = 4

- [ ] **Step 1: Write `web/src/lib/palette.ts`**

```ts
export type Role =
  | "ground" | "surface" | "topBar" | "sunk" | "line" | "text" | "muted" | "accent" | "onAccent" | "tabLine"
  | "succeeded" | "failed" | "running" | "waiting"

// The Forge palette. ghr-theme.css carries the same colours as HSL triplets;
// palette.test.ts keeps the two in step.
export const PALETTE: Record<Role, { light: string; dark: string }> = {
  ground: { light: "#f6f8fa", dark: "#0d1117" },
  surface: { light: "#ffffff", dark: "#151b23" },
  topBar: { light: "#ffffff", dark: "#010409" },
  sunk: { light: "#eaeef2", dark: "#262c36" },
  line: { light: "#d1d9e0", dark: "#3d444d" },
  text: { light: "#1f2328", dark: "#f0f6fc" },
  muted: { light: "#59636e", dark: "#9198a1" },
  accent: { light: "#0969da", dark: "#4493f8" },
  onAccent: { light: "#ffffff", dark: "#0d1117" },
  tabLine: { light: "#fd8c73", dark: "#f78166" },
  succeeded: { light: "#1a7f37", dark: "#3fb950" },
  failed: { light: "#cf222e", dark: "#f85149" },
  running: { light: "#9a6700", dark: "#d29922" },
  waiting: { light: "#bc4c00", dark: "#db6d28" },
}

export const PILL_ALPHA = 0.15

function rgb(hex: string): [number, number, number] {
  const n = Number.parseInt(hex.slice(1), 16)
  return [(n >> 16) & 255, (n >> 8) & 255, n & 255]
}

function luminance(hex: string): number {
  const [r, g, b] = rgb(hex).map((v) => {
    const c = v / 255
    return c <= 0.04045 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4
  }) as [number, number, number]
  return 0.2126 * r + 0.7152 * g + 0.0722 * b
}

export function contrast(a: string, b: string): number {
  const [hi, lo] = [luminance(a), luminance(b)].sort((x, y) => y - x) as [number, number]
  return (hi + 0.05) / (lo + 0.05)
}

// fg painted over bg at the given opacity.
export function over(fg: string, bg: string, alpha: number): string {
  const f = rgb(fg)
  const b = rgb(bg)
  const mix = f.map((v, i) => Math.round(v * alpha + (b[i] ?? 0) * (1 - alpha)))
  return `#${mix.map((v) => v.toString(16).padStart(2, "0")).join("")}`
}

export function hslTriplet(hex: string): string {
  const [r, g, b] = rgb(hex).map((v) => v / 255) as [number, number, number]
  const max = Math.max(r, g, b)
  const min = Math.min(r, g, b)
  const l = (max + min) / 2
  const d = max - min
  let h = 0
  let s = 0
  if (d !== 0) {
    s = d / (1 - Math.abs(2 * l - 1))
    if (max === r) h = ((g - b) / d) % 6
    else if (max === g) h = (b - r) / d + 2
    else h = (r - g) / d + 4
    h *= 60
    if (h < 0) h += 360
  }
  return `${h.toFixed(1)} ${(s * 100).toFixed(1)}% ${(l * 100).toFixed(1)}%`
}
```

- [ ] **Step 2: Write the failing test `web/src/lib/palette.test.ts`**

```ts
import { readFileSync } from "node:fs"
import { dirname, resolve } from "node:path"
import { fileURLToPath } from "node:url"
import { describe, expect, it } from "vitest"
import { contrast, hslTriplet, over, PALETTE, PILL_ALPHA, type Role } from "./palette"

const css = readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), "../styles/ghr-theme.css"), "utf8")
const MODES = ["light", "dark"] as const

function block(mode: string): string {
  const start = css.indexOf(`:root[data-mode="${mode}"] {`)
  return css.slice(start, css.indexOf("\n}", start))
}

function token(mode: string, name: string): string | undefined {
  return new RegExp(`\\n\\s*${name}:\\s*([^;!]+?)\\s*(?:!important)?;`).exec(block(mode))?.[1]
}

const TOKENS: [string, Role][] = [
  ["--background", "ground"],
  ["--card", "surface"],
  ["--muted", "sunk"],
  ["--border", "line"],
  ["--foreground", "text"],
  ["--muted-foreground", "muted"],
  ["--primary", "accent"],
  ["--primary-foreground", "onAccent"],
  ["--success", "succeeded"],
  ["--destructive", "failed"],
  ["--warning", "waiting"],
  ["--running", "running"],
  ["--top-bar", "topBar"],
  ["--tab-line", "tabLine"],
]

describe("the Forge palette", () => {
  it.each(MODES)("is what ghr-theme.css sets in %s mode", (mode) => {
    for (const [name, role] of TOKENS) expect([name, token(mode, name)]).toEqual([name, hslTriplet(PALETTE[role][mode])])
  })

  it.each(MODES)("keeps text and status colours at 4.5:1 on boxes and on the ground in %s mode", (mode) => {
    const roles: Role[] = ["text", "muted", "accent", "succeeded", "failed", "running", "waiting"]
    for (const role of roles) {
      for (const bg of ["surface", "ground"] as const) {
        expect([role, bg, contrast(PALETTE[role][mode], PALETTE[bg][mode]) >= 4.5]).toEqual([role, bg, true])
      }
    }
  })

  it.each(MODES)("keeps a pill's icon at 3:1 on its own tint in %s mode", (mode) => {
    const roles: Role[] = ["accent", "succeeded", "failed", "running", "waiting", "muted"]
    for (const role of roles) {
      for (const bg of ["surface", "ground"] as const) {
        const tint = over(PALETTE[role][mode], PALETTE[bg][mode], PILL_ALPHA)
        expect([role, bg, contrast(PALETTE[role][mode], tint) >= 3]).toEqual([role, bg, true])
        expect([role, bg, contrast(PALETTE.text[mode], tint) >= 4.5]).toEqual([role, bg, true])
      }
    }
  })

  it("computes contrast, blends and triplets exactly", () => {
    expect(contrast("#000000", "#ffffff")).toBeCloseTo(21, 5)
    expect(contrast("#777777", "#777777")).toBe(1)
    expect(contrast("#ffffff", "#767676")).toBeCloseTo(4.54, 2)
    expect(over("#000000", "#ffffff", 0.5)).toBe("#808080")
    expect(over("#ff0000", "#0000ff", 1)).toBe("#ff0000")
    expect(hslTriplet("#ff0000")).toBe("0.0 100.0% 50.0%")
    expect(hslTriplet("#ffffff")).toBe("0.0 0.0% 100.0%")
    expect(hslTriplet("#0969da")).toBe("212.4 92.1% 44.5%")
  })

  it("puts readable text on the primary button", () => {
    for (const mode of MODES) expect(contrast(PALETTE.onAccent[mode], PALETTE.accent[mode])).toBeGreaterThanOrEqual(4.5)
  })
})
```

- [ ] **Step 3: Run it to see it fail**

Run: `cd web && timeout 300 npx vitest run src/lib/palette.test.ts`
Expected: FAIL in "is what ghr-theme.css sets", on `--background`.

- [ ] **Step 4: Rewrite the two mode blocks with this script**

Save as `/tmp/forge-palette.mjs`, run `node /tmp/forge-palette.mjs` from `web/`, then delete it. It sets every listed key in both blocks, fails if one is missing, and adds the three new tokens.

```js
import { readFileSync, writeFileSync } from "node:fs"

const file = "src/styles/ghr-theme.css"
let css = readFileSync(file, "utf8")

const T = {
  light: {
    ground: "210.0 28.6% 97.3%", surface: "0.0 0.0% 100.0%", topBar: "0.0 0.0% 100.0%", sunk: "210.0 23.5% 93.3%",
    line: "208.0 19.5% 84.9%", lineSubtle: "208.0 19.5% 89.0%", lineStrong: "208.0 19.5% 76.0%",
    text: "213.3 12.7% 13.9%", muted: "211.4 10.6% 39.0%", accent: "212.4 92.1% 44.5%", onAccent: "0.0 0.0% 100.0%",
    tabLine: "10.9 97.2% 72.2%", succeeded: "137.2 66.0% 30.0%", failed: "355.8 71.8% 47.3%",
    running: "40.1 100.0% 30.2%", waiting: "24.3 100.0% 36.9%",
  },
  dark: {
    ground: "216.0 27.8% 7.1%", surface: "214.3 25.0% 11.0%", topBar: "217.5 80.0% 2.0%", sunk: "217.5 17.4% 18.0%",
    line: "213.8 11.6% 27.1%", lineSubtle: "213.8 11.6% 22.0%", lineStrong: "213.8 11.6% 33.0%",
    text: "210.0 66.7% 96.5%", muted: "213.8 7.8% 60.0%", accent: "213.7 92.8% 62.0%", onAccent: "216.0 27.8% 7.1%",
    tabLine: "11.2 90.1% 68.4%", succeeded: "128.4 49.2% 48.6%", failed: "2.7 92.6% 62.9%",
    running: "40.6 72.1% 47.8%", waiting: "23.1 71.3% 50.8%",
  },
}

const KEYS = {
  ground: ["--background", "--surface-base"],
  surface: ["--card", "--popover", "--surface-raised", "--surface-overlay", "--surface-header"],
  sunk: ["--muted", "--secondary", "--accent", "--surface-sunken", "--control-well-subtle", "--control-well-deep"],
  line: ["--border", "--input", "--border-default"],
  lineSubtle: ["--border-subtle"],
  lineStrong: ["--border-strong"],
  text: ["--foreground", "--card-foreground", "--popover-foreground", "--secondary-foreground", "--accent-foreground"],
  muted: ["--muted-foreground", "--legend"],
  accent: ["--primary", "--primary-fill", "--ring", "--focus-ring", "--info"],
  onAccent: ["--primary-foreground", "--info-foreground", "--success-foreground", "--destructive-foreground", "--warning-foreground"],
  succeeded: ["--success"],
  failed: ["--destructive"],
  waiting: ["--warning"],
}

for (const mode of ["dark", "light"]) {
  const head = `:root[data-mode="${mode}"] {`
  const start = css.indexOf(head)
  const end = css.indexOf("\n}", start)
  if (start < 0 || end < 0) throw new Error(`no ${mode} block`)
  let body = css.slice(start, end)
  const t = T[mode]
  for (const [role, names] of Object.entries(KEYS)) {
    for (const name of names) {
      const re = new RegExp(`(\\n\\s*${name}:\\s*)[^;]+;`)
      if (!re.test(body)) throw new Error(`${mode} block has no ${name}`)
      body = body.replace(re, `$1${t[role]} !important;`)
    }
  }
  const charts = [
    `hsl(${t.accent})`, `hsl(${t.accent} / 0.62)`, `hsl(${t.accent} / 0.4)`, `hsl(${t.accent} / 0.24)`, `hsl(${t.muted})`,
  ]
  charts.forEach((value, i) => {
    const re = new RegExp(`(\\n\\s*--chart-${i + 1}:\\s*)[^;]+;`)
    if (!re.test(body)) throw new Error(`${mode} block has no --chart-${i + 1}`)
    body = body.replace(re, `$1${value} !important;`)
  })
  body += `\n  --running: ${t.running};\n  --top-bar: ${t.topBar};\n  --tab-line: ${t.tabLine};`
  css = css.slice(0, start) + body + css.slice(end)
}

css += `\n@theme inline {\n  --color-running: hsl(var(--running));\n}\n`
writeFileSync(file, css)
```

- [ ] **Step 5: Run the tests**

Run: `cd web && timeout 300 npx vitest run src/lib/palette.test.ts && timeout 600 npm test`
Expected: PASS, including `src/styles/theme.test.ts`, whose contrast pairs the new values meet. If another test asserts an old colour value, update that assertion to the new token's value.

- [ ] **Step 6: Commit**

```bash
git add web/src/lib/palette.ts web/src/lib/palette.test.ts web/src/styles/ghr-theme.css
git commit -m "feat(web): set the Forge palette"
```

### Task 3: System colour mode by default

**Files:**
- Modify: `web/public/theme-init.js` (the `var dark =` statement)
- Modify: `web/src/theme.config.ts` (`defaults.mode`)
- Test: `web/src/theme-init.test.ts`, `web/src/styles/theme.test.ts`, `web/src/components/mode-control.test.tsx`

**Interfaces:**
- Consumes: nothing.
- Produces: nothing other tasks use.

**Items:** 4

**Depends on:** Task 2

**Implementer:** dr-superpowers:impl-sonnet-low
**Evaluation:** files 1 - spec 0 - coupling 0 - risk 1 = 2

- [ ] **Step 1: Replace the "falls back to dark" test in `web/src/theme-init.test.ts`**

Delete the test named "falls back to dark" and add these in its place, inside the existing `describe`:

```ts
  function prefersDark(matches: boolean) {
    const original = window.matchMedia
    Object.defineProperty(window, "matchMedia", {
      writable: true,
      configurable: true,
      value: (query: string) => ({ matches: query.includes("dark") ? matches : false, media: query, addEventListener() {}, removeEventListener() {} }),
    })
    return () => Object.defineProperty(window, "matchMedia", { writable: true, configurable: true, value: original })
  }

  it.each([
    [true, "dark"],
    [false, "light"],
  ])("follows the system when no mode is saved (prefers dark: %s)", (matches, expected) => {
    const restore = prefersDark(matches)
    try {
      run()
      expect(root.getAttribute("data-mode")).toBe(expected)
    } finally {
      restore()
    }
  })

  it.each([
    ["dark", false],
    ["light", true],
  ])("keeps a saved %s mode whatever the system prefers", (saved, systemDark) => {
    const restore = prefersDark(systemDark)
    try {
      localStorage.setItem("mode", saved)
      run()
      expect(root.getAttribute("data-mode")).toBe(saved)
    } finally {
      restore()
    }
  })
```

- [ ] **Step 2: Run it to see it fail**

Run: `cd web && timeout 300 npx vitest run src/theme-init.test.ts`
Expected: FAIL on "follows the system when no mode is saved (prefers dark: false)": received `dark`.

- [ ] **Step 3: Change `web/public/theme-init.js`**

Replace the `var dark = …` statement with:

```js
  var dark =
    mode === "dark" ? true : mode === "light" ? false : window.matchMedia("(prefers-color-scheme: dark)").matches
```

- [ ] **Step 4: Change `web/src/theme.config.ts`**

Set `mode: "system",` in `defaults`.

- [ ] **Step 5: Update the two tests that pin the old default**

- `web/src/styles/theme.test.ts`, in "pins every axis and turns the switcher off": change `mode: "dark",` to `mode: "system",`.
- `web/src/components/mode-control.test.tsx`: rename "starts on dark and saves the chosen mode" to "starts on system and saves the chosen mode". If it asserts which radio is checked at the start, expect the one named "System". Leave the rest of the test as it is.

- [ ] **Step 6: Run the checks**

Run: `cd web && timeout 300 npx vitest run src/theme-init.test.ts src/styles/theme.test.ts src/components/mode-control.test.tsx && timeout 300 npm run typecheck && timeout 600 npm test`
Expected: PASS, and the typecheck accepts `mode: "system"`. If the typecheck rejects it, stop and report: the kit's `ThemeConfig` then needs another way to default to the system.

- [ ] **Step 7: Commit**

```bash
git add web/public/theme-init.js web/src/theme.config.ts web/src/theme-init.test.ts web/src/styles/theme.test.ts web/src/components/mode-control.test.tsx
git commit -m "feat(web): follow the system colour mode by default"
```

### Task 4: Status pill and tones

**Files:**
- Modify: `web/src/lib/status.ts` (`Tone`, `tones`; add `stateWord`)
- Create: `web/src/lib/state-icon.ts`
- Create: `web/src/components/page/status-pill.tsx`
- Test: `web/src/components/page/status-pill.test.tsx`
- Modify: `web/src/components/page/state-text.tsx`, `web/src/components/page/state-text.test.tsx`
- Modify: `web/src/components/result-icon.tsx`, `web/src/components/result-icon.test.tsx`, `web/src/components/run-status-icon.tsx`

**Interfaces:**
- Consumes: the `running` colour (Contracts, "Tokens and classes").
- Produces: Contracts, "Status".

**Items:** 1

**Depends on:** Task 2

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 1 = 4

- [ ] **Step 1: Write the failing test `web/src/components/page/status-pill.test.tsx`**

```tsx
import { render, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import { ResultIcon } from "@/components/result-icon"
import { RunStatusIcon } from "@/components/run-status-icon"
import { stateTone, stateWord } from "@/lib/status"
import { StatusPill } from "./status-pill"

describe("the status vocabulary", () => {
  it.each([
    ["success", "Succeeded", "ok", "text-success", "lucide-check"],
    ["succeeded", "Succeeded", "ok", "text-success", "lucide-check"],
    ["failure", "Failed", "bad", "text-destructive", "lucide-x"],
    ["failed", "Failed", "bad", "text-destructive", "lucide-x"],
    ["error", "Error", "bad", "text-destructive", "lucide-x"],
    ["running", "Running", "run", "text-running", "lucide-circle-dot"],
    ["busy", "Running", "run", "text-running", "lucide-circle-dot"],
    ["in_progress", "Running", "run", "text-running", "lucide-circle-dot"],
    ["queued", "Waiting", "warn", "text-warning", "lucide-clock"],
    ["waiting", "Waiting", "warn", "text-warning", "lucide-clock"],
    ["cancelled", "Cancelled", "muted", "text-muted-foreground", "lucide-ban"],
    ["skipped", "Skipped", "muted", "text-muted-foreground", "lucide-minus"],
    ["idle", "Idle", "muted", "text-muted-foreground", "lucide-minus"],
    ["paused", "Paused", "muted", "text-muted-foreground", "lucide-minus"],
    ["online", "Online", "accent", "text-primary", "lucide-dot"],
  ])("draws %s as %s", (state, word, tone, iconClass, shape) => {
    expect(stateWord(state)).toBe(word)
    expect(stateTone(state)).toBe(tone)
    const { container } = render(<StatusPill state={state} />)
    expect(screen.getByText(word)).toHaveClass("text-foreground")
    const icon = container.querySelector("svg")
    expect(icon).toHaveClass(iconClass, shape)
    expect(icon).toHaveAttribute("aria-hidden", "true")
  })

  it("shows a given label in place of the state's word", () => {
    render(<StatusPill state="success" label="Up to date" />)
    expect(screen.getByText("Up to date")).toBeInTheDocument()
    expect(screen.queryByText("Succeeded")).not.toBeInTheDocument()
  })

  it.each([
    ["success", "Succeeded", "text-success"],
    ["failure", "Failed", "text-destructive"],
    ["cancelled", "Cancelled", "text-muted-foreground"],
    ["skipped", "Skipped", "text-muted-foreground"],
    ["unknown", "Unknown", "text-muted-foreground"],
    ["timed_out", "Failed", "text-destructive"],
  ])("names a %s result %s", (conclusion, word, iconClass) => {
    render(<ResultIcon conclusion={conclusion} />)
    expect(screen.getByRole("img", { name: word })).toHaveClass(iconClass)
  })

  it("names a queued run Waiting, in the waiting colour", () => {
    render(<RunStatusIcon status="queued" conclusion="" />)
    expect(screen.getByRole("img", { name: "Waiting" })).toHaveClass("text-warning")
  })

  it("keeps the spinner for a run in progress", () => {
    render(<RunStatusIcon status="in_progress" conclusion="" />)
    expect(screen.getByText("Running")).toHaveClass("sr-only")
  })
})
```

- [ ] **Step 2: Run it to see it fail**

Run: `cd web && timeout 300 npx vitest run src/components/page/status-pill.test.tsx`
Expected: FAIL, cannot resolve `./status-pill`.

- [ ] **Step 3: Change `web/src/lib/status.ts`**

Replace the `Tone` type and the `tones` map, and add `stateWord` after `stateTone`. `running`, `repoStateWord`, `stateTone` and `waitingRepos` stay as they are.

```ts
export type Tone = "accent" | "run" | "warn" | "bad" | "ok" | "muted"

const tones: Record<string, Tone> = {
  busy: "run",
  running: "run",
  active: "run",
  in_progress: "run",
  online: "accent",
  matched: "accent",
  waiting: "warn",
  starting: "warn",
  queued: "warn",
  pending: "warn",
  requested: "warn",
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
  succeeded: "ok",
  valid: "ok",
  "up to date": "ok",
}
```

```ts
// One word per state, whatever the API calls it.
const words: Record<string, string> = {
  busy: "Running",
  in_progress: "Running",
  queued: "Waiting",
  pending: "Waiting",
  requested: "Waiting",
  success: "Succeeded",
  failure: "Failed",
}

export function stateWord(state: string): string {
  const known = words[state.toLowerCase()]
  if (known) return known
  const text = state.replace(/_/g, " ")
  return text.charAt(0).toUpperCase() + text.slice(1)
}
```

- [ ] **Step 4: Write `web/src/lib/state-icon.ts`**

```ts
import { Ban, Check, CircleDot, CircleHelp, Clock, Dot, Minus, X, type LucideIcon } from "lucide-react"
import { stateTone, type Tone } from "@/lib/status"

const BY_TONE: Record<Tone, LucideIcon> = { ok: Check, bad: X, run: CircleDot, warn: Clock, accent: Dot, muted: Minus }
const BY_STATE: Record<string, LucideIcon | undefined> = { cancelled: Ban, unknown: CircleHelp }

export const TONE_TEXT: Record<Tone, string> = {
  ok: "text-success",
  bad: "text-destructive",
  run: "text-running",
  warn: "text-warning",
  accent: "text-primary",
  muted: "text-muted-foreground",
}

export const TONE_FILL: Record<Tone, string> = {
  ok: "bg-success/15",
  bad: "bg-destructive/15",
  run: "bg-running/15",
  warn: "bg-warning/15",
  accent: "bg-primary/15",
  muted: "bg-muted-foreground/15",
}

export function stateIcon(state: string): LucideIcon {
  return BY_STATE[state.toLowerCase()] ?? BY_TONE[stateTone(state)]
}
```

- [ ] **Step 5: Write `web/src/components/page/status-pill.tsx`**

```tsx
import { stateIcon, TONE_FILL, TONE_TEXT } from "@/lib/state-icon"
import { stateTone, stateWord } from "@/lib/status"

// The word stays in the text colour: a coloured word on its own tint falls
// under 4.5:1 for light-mode Running.
export function StatusPill({ state, label }: { state: string; label?: string }) {
  const tone = stateTone(state)
  const Icon = stateIcon(state)
  return (
    <span className={`inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-xs font-semibold whitespace-nowrap ${TONE_FILL[tone]}`}>
      <Icon size={13} aria-hidden="true" className={`shrink-0 ${TONE_TEXT[tone]}`} />
      <span className="text-foreground">{label ?? stateWord(state)}</span>
    </span>
  )
}
```

- [ ] **Step 6: Draw the result and run icons from the same mapping**

Replace the whole of `web/src/components/result-icon.tsx`:

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

In `web/src/components/run-status-icon.tsx`, replace the last line of the function (the muted `Clock` labelled "Queued") with:

```tsx
  return <Clock role="img" aria-label="Waiting" size={15} className="shrink-0 text-warning" />
```

The `in_progress` spinner line stays as it is.

- [ ] **Step 7: Make `StateText` the pill**

Replace the whole of `web/src/components/page/state-text.tsx` with:

```tsx
// Kept so call sites compile until they are renamed to StatusPill.
export { StatusPill as StateText } from "./status-pill"
```

In `web/src/components/page/state-text.test.tsx`, delete assertions about colour classes on the word; the pill test covers colour.

- [ ] **Step 8: Run the tests and update what the vocabulary reaches**

Run: `cd web && timeout 600 npm test && timeout 300 npm run typecheck && timeout 300 npm run lint`
Expected: failures only where a test names an old word, icon or class. Update each to the table in Step 1:

- a pill that read "Busy" reads "Running"; "Queued" reads "Waiting"; "Success" reads "Succeeded"; "Failure" reads "Failed";
- an icon labelled "Queued" is labelled "Waiting";
- a running, busy or active state expects `text-running` where it expected `text-primary`;
- a skipped result's icon is `lucide-minus` where a test names `lucide-circle-slash`;
- a `stateTone` case for busy, running or active expects `"run"` where it expected `"accent"`;
- a `Record<Tone, …>` the typecheck reports as missing `run` gains `run` with that map's `running` class (`text-running`, `bg-running`, `fill-running` or `stroke-running`).

Change no test for any other reason. Re-run until all three commands pass.

- [ ] **Step 9: Commit**

```bash
git add web/src/lib/status.ts web/src/lib/state-icon.ts web/src/components/page/status-pill.tsx web/src/components/page/status-pill.test.tsx web/src/components/page/state-text.tsx web/src/components/page/state-text.test.tsx web/src/components/result-icon.tsx web/src/components/result-icon.test.tsx web/src/components/run-status-icon.tsx
git add -u web/src
git commit -m "feat(web): show states as pills with one vocabulary"
```

### Task 5: Box section

**Files:**
- Modify: `web/src/components/page/section.tsx`
- Modify: `web/src/styles/ghr-theme.css` (the radius tokens)
- Test: `web/src/components/page/section.test.tsx`, `web/src/styles/theme.test.ts`

**Interfaces:**
- Consumes: nothing.
- Produces: Contracts, "Box".

**Items:** 1

**Depends on:** Task 2

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 1 = 4

- [ ] **Step 1: Add the failing tests to `web/src/components/page/section.test.tsx`**

```tsx
  it("draws a header band above a padded body", () => {
    render(
      <Section title="Events" aside={<span>aside</span>}>
        <p>body</p>
      </Section>,
    )
    const heading = screen.getByRole("heading", { level: 2, name: "Events" })
    expect(heading.parentElement).toHaveClass("bg-background", "border-b")
    expect(heading.parentElement).toContainElement(screen.getByText("aside"))
    expect(screen.getByText("body").parentElement).toHaveClass("p-4")
  })

  it("drops the body padding when flush", () => {
    render(
      <Section title="Runs" flush>
        <p>body</p>
      </Section>,
    )
    expect(screen.getByText("body").parentElement).not.toHaveClass("p-4")
  })
```

Add `screen` and `render` to the file's imports if they are missing.

- [ ] **Step 2: Run it to see it fail**

Run: `cd web && timeout 300 npx vitest run src/components/page/section.test.tsx`
Expected: FAIL on "draws a header band above a padded body".

- [ ] **Step 3: Replace `web/src/components/page/section.tsx`**

```tsx
import { useId, type ReactNode } from "react"

export function Section({
  id,
  title,
  aside,
  children,
  className = "",
  flush = false,
}: {
  id?: string
  title: string
  aside?: ReactNode
  children: ReactNode
  className?: string
  flush?: boolean
}) {
  const headingId = useId()
  return (
    <section id={id} aria-labelledby={headingId} className={`min-w-0 overflow-hidden rounded-md border border-border bg-card ${className}`}>
      <div className="flex flex-wrap items-center gap-x-3 gap-y-1 border-b border-border bg-background px-4 py-2.5">
        <h2 id={headingId} className="text-sm font-semibold">
          {title}
        </h2>
        {aside}
      </div>
      <div className={flush ? "" : "p-4"}>{children}</div>
    </section>
  )
}
```

- [ ] **Step 4: One radius for kit surfaces too**

In `web/src/styles/ghr-theme.css`, in the `@theme inline` block that sets the radii, set `--radius-lg: 6px;` and `--radius-xl: 6px;` (`--radius-sm: 4px` and `--radius-md: 6px` stay), and change the `:root[data-radius]` rule to:

```css
:root[data-radius] {
  --radius: 6px;
  --radius-button: 6px;
}
```

In `web/src/styles/theme.test.ts`, replace the test "pins two radii: 6px on controls and 10px on surfaces" with:

```ts
  it("pins one radius: 6px on controls and on surfaces", () => {
    expect(block(":root[data-radius]")).toEqual({ "--radius": "6px", "--radius-button": "6px" })
    expect(css).toMatch(/--radius-md: 6px;/)
    expect(css).toMatch(/--radius-lg: 6px;/)
    expect(css).toMatch(/--radius-xl: 6px;/)
  })
```

- [ ] **Step 5: Run the tests**

Run: `cd web && timeout 600 npm test`
Expected: PASS. If an older assertion in `section.test.tsx` names a removed class (`rounded-[10px]`, `p-4` on the section, `text-base`), update it to the class above that replaced it.

- [ ] **Step 6: Commit**

```bash
git add web/src/components/page/section.tsx web/src/components/page/section.test.tsx web/src/styles/ghr-theme.css web/src/styles/theme.test.ts
git commit -m "feat(web): draw sections as boxes with a header band"
```

### Task 6: Update banner

**Files:**
- Modify: `web/src/components/update-card.tsx` (the returned `<section>` and its heading)
- Test: `web/src/components/update-card.test.tsx`

**Interfaces:**
- Consumes: nothing.
- Produces: Contracts, "Update banner".

**Items:** 3

**Depends on:** none

**Implementer:** dr-superpowers:impl-sonnet-low
**Evaluation:** files 1 - spec 0 - coupling 0 - risk 0 = 1

- [ ] **Step 1: Add the failing test to `web/src/components/update-card.test.tsx`**

The file has `draw(update, offline?)` and `inDays(days)`.

```tsx
  it("lays out as one horizontal banner", () => {
    draw({ installed: "2.337.0", latest: "2.338.0", deadline: inDays(10) })
    const banner = screen.getByRole("region", { name: "Runner update" })
    expect(banner).toHaveClass("flex-wrap", "items-center", "rounded-md")
    expect(banner).not.toHaveClass("flex-col")
    expect(within(banner).getByRole("button")).toBeInTheDocument()
  })
```

Add `within` to the `@testing-library/react` import if it is missing.

- [ ] **Step 2: Run it to see it fail**

Run: `cd web && timeout 300 npx vitest run src/components/update-card.test.tsx`
Expected: FAIL: the banner has class `flex-col`.

- [ ] **Step 3: Change the wrapper in `web/src/components/update-card.tsx`**

Replace the `<section>` opening tag's `className` with:

```tsx
      className={`flex flex-wrap items-center gap-x-3 gap-y-2 rounded-md border bg-card px-4 py-2.5 text-sm text-card-foreground ${urgent ? "border-destructive" : "border-warning"}`}
```

Leave the heading, the paragraphs, the buttons and their conditions unchanged.

- [ ] **Step 4: Run the test**

Run: `cd web && timeout 300 npx vitest run src/components/update-card.test.tsx`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add web/src/components/update-card.tsx web/src/components/update-card.test.tsx
git commit -m "feat(web): lay the runner update out as a banner"
```

### Task 7: Top-bar shell

**Files:**
- Modify: `web/src/components/shell.tsx`
- Modify: `web/src/components/capacity-bar.tsx`
- Modify: `web/src/styles/ghr-theme.css` (the shell rules)
- Test: `web/src/components/shell.test.tsx`, `web/src/components/capacity-bar.test.tsx`, `web/src/styles/theme.test.ts`

**Interfaces:**
- Consumes: `--top-bar`, `--tab-line` and the `running` colour (Contracts, "Tokens and classes"); `UpdateCard` (Contracts, "Update banner").
- Produces: Contracts, "Shell".

**Items:** 3

**Depends on:** Task 3, Task 6

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 2 = 4

The kit's `TopNavLayout` is not used: it adds a search box and a user menu, shows no badges, and hides its tabs below 768px (`web/node_modules/darkraise-ui/dist`, checked 2026-10-10). The shell is ghr's own markup.

- [ ] **Step 1: Rewrite the shell tests that name the sidebar**

In `web/src/components/shell.test.tsx` replace these tests with the versions below, and leave every other test in the file as it is.

Replace "lists the pages in two groups":

```tsx
  it("lists the pages as tabs, without Toolchains or Storage", async () => {
    mockApi(authedRoutes())
    renderApp("/")
    const nav = await screen.findByRole("navigation", { name: "Pages" })
    await waitFor(() => expect(within(nav).getByRole("link", { name: /^Repositories/ })).toBeInTheDocument())
    expect(within(nav).getAllByRole("link").map((a) => a.textContent?.replace(/\d+$/, ""))).toEqual([
      "Dashboard",
      "Runners",
      "Actions",
      "History",
      "Repositories",
      "Settings",
    ])
    expect(within(nav).getByRole("link", { name: /^Dashboard/ })).toHaveAttribute("aria-current", "page")
    expect(within(nav).getByRole("link", { name: /^Settings/ })).toHaveAttribute("href", "/settings")
  })

  it("keeps a tab current on its nested pages", async () => {
    mockApi(runRoutes())
    renderApp("/actions/darkmem/runs/102")
    const nav = await screen.findByRole("navigation", { name: "Pages" })
    expect(within(nav).getByRole("link", { name: /^Actions/ })).toHaveAttribute("aria-current", "page")
    expect(within(nav).getByRole("link", { name: /^Dashboard/ })).not.toHaveAttribute("aria-current")
  })
```

Replace "counts live runners and configured repositories in the nav" (same expectations, scoped to the tabs):

```tsx
  it("counts live runners and configured repositories in the tabs", async () => {
    mockApi(authedRoutes())
    renderApp("/")
    const nav = await screen.findByRole("navigation", { name: "Pages" })
    await waitFor(() => expect(within(within(nav).getByRole("link", { name: /^Runners/ })).getByText("2")).toBeInTheDocument())
    await waitFor(() => expect(within(within(nav).getByRole("link", { name: /^Repositories/ })).getByText("2")).toBeInTheDocument())
  })
```

Import `runRoutes` from `@/test/fixtures` beside `authedRoutes`.

Rename "logs out from the rail" to "logs out from the top bar" (its body is unchanged).

Replace "logs out from the header's user menu past unsaved edits":

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

Replace "puts the capacity readout and the user menu in the header bar":

```tsx
  it("puts the brand, the host, the capacity and the mode control in the top bar", async () => {
    mockApi(authedRoutes())
    renderApp("/")
    const bar = await screen.findByRole("banner")
    await waitFor(() => expect(within(bar).getByText("1 of 2 busy")).toBeInTheDocument())
    expect(within(bar).getByRole("img", { name: "1 of 2 busy, 1 warm" })).toBeInTheDocument()
    expect(within(bar).getByText("localhost")).toBeInTheDocument()
    expect(within(bar).getByRole("radiogroup", { name: "Colour mode" })).toBeInTheDocument()
    expect(within(bar).getByRole("button", { name: "Log out" })).toBeInTheDocument()
    expect(within(bar).queryByRole("searchbox")).not.toBeInTheDocument()
  })
```

Replace "names the host for the brand label":

```tsx
  it("shows the runner update as a banner above the page", async () => {
    mockApi(authedRoutes())
    renderApp("/")
    const banner = await screen.findByRole("region", { name: "Runner update" })
    expect(screen.getByRole("main")).toContainElement(banner)
    expect(screen.getByRole("banner")).not.toContainElement(banner)
  })

  it("keeps the main-content target and a skip link to it", async () => {
    mockApi(authedRoutes())
    renderApp("/")
    const main = await screen.findByRole("main")
    expect(main).toHaveAttribute("id", "main-content")
    expect(main).toHaveAttribute("tabindex", "-1")
    expect(screen.getByRole("link", { name: "Skip to content" })).toHaveAttribute("href", "#main-content")
  })
```

Run: `cd web && timeout 300 npx vitest run src/components/shell.test.tsx`
Expected: FAIL: no navigation named "Pages". The main-content case is a guard and may already pass: the old layout provides that id.

- [ ] **Step 2: Rewrite the layout in `web/src/components/shell.tsx`**

Remove the imports of `SidebarLayout`, `useBrandStore`, `NavGroup`, `HardDrive`, `Wrench` and `CapacityBar`. Add:

```tsx
import type { LucideIcon } from "lucide-react"
import { Brand } from "@/components/brand"
import { CapacitySegments } from "@/components/capacity-bar"
```

Replace `navGroups` with:

```tsx
interface Tab {
  label: string
  to: "/" | "/runners" | "/actions" | "/history" | "/repositories" | "/settings"
  icon: LucideIcon
  badge?: string
  divided?: boolean
}

function tabs(status: Status | undefined, config: Config | undefined): Tab[] {
  return [
    { label: "Dashboard", to: "/", icon: LayoutDashboard },
    { label: "Runners", to: "/runners", icon: Boxes, badge: count(status && running(status)) },
    { label: "Actions", to: "/actions", icon: Workflow },
    { label: "History", to: "/history", icon: HistoryIcon },
    { label: "Repositories", to: "/repositories", icon: GitBranch, badge: count(config?.repos?.length), divided: true },
    { label: "Settings", to: "/settings", icon: Settings },
  ]
}

const TAB =
  "flex shrink-0 items-center gap-2 rounded-t-md border-b-2 border-transparent px-3 py-2 text-sm whitespace-nowrap hover:bg-muted aria-[current=page]:border-[hsl(var(--tab-line))] aria-[current=page]:font-semibold"
const FRAME = "mx-auto w-full max-w-[1264px] px-6"
```

Delete the effect that calls `useBrandStore` and sets `--ghr-host`. Keep every other hook, the epoch effect and `logout`. Replace the returned JSX with:

```tsx
    <div className="flex min-h-dvh flex-col bg-background text-foreground">
      <a
        href="#main-content"
        className="sr-only focus:not-sr-only focus:absolute focus:top-2 focus:left-2 focus:z-50 focus:rounded-md focus:bg-card focus:px-3 focus:py-2 focus:text-sm"
      >
        Skip to content
      </a>
      <header className="border-b border-border bg-[hsl(var(--top-bar))]">
        <div className={`${FRAME} flex h-14 items-center gap-3`}>
          <Link to="/" aria-label="ghr" className="flex items-center rounded-md">
            <Brand />
          </Link>
          <span className="hidden font-mono text-xs text-muted-foreground sm:inline">{window.location.hostname}</span>
          <span className="flex-1" />
          {st && (
            <span className="flex items-center gap-2">
              <span className="w-20">
                <CapacitySegments status={st} />
              </span>
              <span className="hidden font-mono text-sm sm:inline">{capacity(st).text}</span>
            </span>
          )}
          <ModeControl />
          <Button size="sm" variant="ghost" aria-label="Log out" onClick={() => void logout()}>
            <LogOut size={15} aria-hidden="true" />
            <span className="hidden sm:inline">Log out</span>
          </Button>
        </div>
        <nav aria-label="Pages" className={`${FRAME} flex gap-1 overflow-x-auto`}>
          {tabs(st, config.data).map((tab) => (
            <span key={tab.to} className={`flex shrink-0 ${tab.divided ? "ml-2 border-l border-border pl-2" : ""}`}>
              <Link to={tab.to} activeOptions={{ exact: tab.to === "/" }} className={TAB}>
                <tab.icon size={15} aria-hidden="true" />
                {tab.label}
                {tab.badge && <span className="rounded-full bg-muted px-1.5 font-mono text-xs">{tab.badge}</span>}
              </Link>
            </span>
          ))}
        </nav>
      </header>
      <main id="main-content" tabIndex={-1} className={`${FRAME} flex flex-1 flex-col py-6 outline-none`}>
        {/* the three existing Alert banners (Reconnecting, Degraded, First-run setup), unchanged, in their current order */}
        {st && (
          <div className="mb-4 empty:hidden">
            <UpdateCard update={st.runner_update} offline={offline} />
          </div>
        )}
        <Outlet />
      </main>
    </div>
```

Move the three existing `Alert` blocks to where the comment stands, unchanged. `main` keeps the id `main-content` the kit's layout gave it: `web/src/a11y.test.tsx:10-11` finds the page by that id, and the skip link targets it. `FRAME`'s 1264px is the 1216px content column plus the 24px side padding on each side.

- [ ] **Step 3: Retire the rail in `web/src/components/capacity-bar.tsx`**

Delete `CapacityBar` and the `tone` prop, so free segments are always `bg-muted`, and change the colours:

```tsx
const KIND_CLASS: Record<Exclude<SegmentKind, "free">, string> = {
  busy: "ghr-pulse bg-running",
  starting: "bg-running/45",
  warm: "border border-running",
}
```

In `web/src/components/capacity-bar.test.tsx`, delete the `CapacityBar` and rail-tone cases and expect `bg-running` and `border-running` where a case expects `bg-primary` and `border-primary`. In `web/src/components/stat-card.tsx` nothing changes (it passes no `tone`); if any other caller passes `tone`, remove the prop there.

- [ ] **Step 4: Replace the sidebar rules in `web/src/styles/ghr-theme.css`**

Delete these rules: `.dr-sidebar-nav-icon` and `.dr-sidebar-nav-icon-svg`; `.dr-sidebar-nav-item.active`; `.dr-sidebar-nav-badge`; `.dr-brand-logo-label` and its `::after`; the `:root { --sidebar-width-expanded: 18rem; }` rule and its comment; `.dr-sidebar-layout-toggle`; `.dr-layout-header`; and the `@media (min-width: 768px)` rule that hides `.dr-sidebar-layout > .dr-layout-header`.

Keep the `--surface-sidebar` and `--sidebar-*` lines inside the two mode blocks: the theme engine writes those keys, and `styles/theme.test.ts` requires an override for each.

Change the selector of the light-mode rail block from

```css
:root[data-mode="light"] .dr-sidebar-layout-aside,
:root[data-mode="light"] .dr-mobile-drawer-content {
```

to

```css
:root[data-mode="light"] .ghr-log-panel {
```

and replace its comment with: `/* The log panel is dark in both modes. Kit controls inside it read the page tokens, so in light mode the panel rebinds them to the dark palette. */`. Inside that block, set each HSL-triplet property to the value the same property has in the `:root[data-mode="dark"]` block (without `!important`); leave the lines whose value is `hsl(var(--…))` as they are; and add these three lines:

```css
  --running: 40.6 72.1% 47.8%;
  --control-surface: var(--card);
  --control-well: var(--card);
```

The kit computes `--control-surface` from `--card` on `:root` (`web/node_modules/darkraise-ui/dist/styles.css`, the `--control-surface: var(--card)` declarations), so without the last two lines an input inside the panel keeps the light card colour.

- [ ] **Step 5: Update `web/src/styles/theme.test.ts`**

In `describe("ghr chrome styles", …)`:

- Replace "keeps the light-mode rail readable with the dark palette" with:

```ts
  it("keeps the light-mode log panel readable with the dark palette", () => {
    const light = tokens("light")
    const panel = { ...light, ...block(':root[data-mode="light"] .ghr-log-panel') }
    const checks: [string, number][] = [
      ["--foreground", 4.5],
      ["--muted-foreground", 4.5],
      ["--warning", 4.5],
      ["--destructive", 4.5],
      ["--success", 4.5],
      ["--running", 4.5],
      ["--primary", 3],
    ]
    for (const [fg, min] of checks) {
      expect(contrast(panel[fg], light["--log-bg"]), `${fg} on the log`).toBeGreaterThanOrEqual(min)
      expect(contrast(panel[fg], panel["--card"]), `${fg} on a panel card`).toBeGreaterThanOrEqual(min)
    }
  })
```

- In "recomputes the kit's surface fills inside the rail", rename it to "recomputes the kit's surface fills inside the log panel", change the selector string to `':root[data-mode="light"] .ghr-log-panel'`, and add two assertions: `expect(rail["--control-surface"]).toBe("var(--card)")` and `expect(rail["--control-well"]).toBe("var(--card)")`.
- Delete "hides the header bar at 768px and wider", "hides the rail's collapse toggle", "widens the rail for the large font step" and "appends the host to the brand label".

- [ ] **Step 6: Run the checks**

Run: `cd web && timeout 600 npm test && timeout 300 npm run typecheck && timeout 300 npm run lint && timeout 600 npm run build`
Expected: PASS. A page test that looked for something inside `.dr-layout-header`, the sidebar, or a "Toolchains" or "Storage" navigation link now fails; point it at the `banner` landmark or the "Pages" navigation instead, and open Toolchains and Storage by their addresses.

- [ ] **Step 7: Commit**

```bash
git add web/src/components/shell.tsx web/src/components/shell.test.tsx web/src/components/capacity-bar.tsx web/src/components/capacity-bar.test.tsx web/src/styles/ghr-theme.css web/src/styles/theme.test.ts
git add -u web/src
git commit -m "feat(web): replace the sidebar with a top bar and tabs"
```

### Task 8: Run row and side layout

**Files:**
- Create: `web/src/lib/run-cols.ts`, `web/src/lib/side-link.ts`
- Create: `web/src/components/page/run-row.tsx`, `web/src/components/page/side-layout.tsx`
- Test: `web/src/components/page/run-row.test.tsx`, `web/src/components/page/side-layout.test.tsx`

**Interfaces:**
- Consumes: nothing.
- Produces: Contracts, "Rows and side layout".

**Items:** 5, 6, 7, 8

**Depends on:** none

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 0 = 3

- [ ] **Step 1: Write the failing tests**

`web/src/components/page/run-row.test.tsx`:

```tsx
import { render, screen, within } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import { runCols } from "@/lib/run-cols"
import { RunGroup, RunRow, RunValue } from "./run-row"

function table(wide: boolean, actions = false) {
  return render(
    <table>
      <tbody>
        <RunGroup label="Today" cols={runCols(wide, actions)} />
        <RunRow
          wide={wide}
          icon={<span>icon</span>}
          title={<a href="/x">build</a>}
          facts={<span>CI #7</span>}
          values={
            <>
              <RunValue kind="time">14:02</RunValue>
              <RunValue kind="duration">3m10s</RunValue>
            </>
          }
          actions={actions ? <button type="button">Open</button> : undefined}
        />
      </tbody>
    </table>,
  )
}

const cells = () => within(screen.getAllByRole("row")[1] as HTMLElement).getAllByRole("cell")

describe("RunRow", () => {
  it("puts the values in their own right-aligned cell when wide", () => {
    table(true)
    expect(cells()).toHaveLength(3)
    expect(cells()[1]).toHaveTextContent("buildCI #7")
    expect(cells()[2]).toHaveTextContent("14:023m10s")
    expect(cells()[2]).toHaveClass("text-right")
    expect(cells()[2]?.firstElementChild).toHaveClass("items-end")
  })

  it("moves the values under the facts when narrow", () => {
    table(false)
    expect(cells()).toHaveLength(2)
    expect(cells()[1]).toHaveTextContent("buildCI #714:023m10s")
  })

  it("adds an actions cell only when actions are given", () => {
    table(true, true)
    expect(cells()).toHaveLength(4)
    expect(screen.getByRole("button", { name: "Open" })).toBeInTheDocument()
  })

  it("spans the group heading across the row", () => {
    table(true, true)
    const heading = screen.getByRole("rowheader", { name: "Today" })
    expect(heading).toHaveAttribute("scope", "rowgroup")
    expect(heading).toHaveAttribute("colspan", "4")
  })

  it("counts the columns", () => {
    expect([runCols(true, false), runCols(true, true), runCols(false, false), runCols(false, true)]).toEqual([3, 4, 2, 3])
  })
})
```

`web/src/components/page/side-layout.test.tsx`:

```tsx
import { render, screen, within } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import { SIDE_LINK } from "@/lib/side-link"
import { SideGroup, SideLayout } from "./side-layout"

describe("SideLayout", () => {
  it.each([
    ["Settings", "General", "Host", "the general settings"],
    ["Repository", "Overview", "More", "the overview"],
  ])("names the %s navigation and shows the content beside it", (label, link, group, content) => {
    const { container } = render(
      <SideLayout
        label={label}
        nav={
          <>
            <a href="/a" aria-current="page" className={SIDE_LINK}>
              {link}
            </a>
            <SideGroup>{group}</SideGroup>
          </>
        }
      >
        <p>{content}</p>
      </SideLayout>,
    )
    const nav = screen.getByRole("navigation", { name: label })
    expect(within(nav).getByRole("link", { name: link })).toHaveAttribute("aria-current", "page")
    expect(within(nav).getByText(group)).toBeInTheDocument()
    expect(nav).not.toContainElement(screen.getByText(content))
    expect(container.firstElementChild).toHaveClass("grid", "min-[800px]:grid-cols-[15rem_minmax(0,1fr)]")
    expect(container.firstElementChild).not.toHaveClass("grid-cols-[15rem_minmax(0,1fr)]")
  })

  it("marks the current link by its aria-current attribute", () => {
    expect(SIDE_LINK).toContain("aria-[current=page]:bg-muted")
    expect(SIDE_LINK).toContain("aria-[current=page]:font-semibold")
    expect(SIDE_LINK).toContain("aria-[current=page]:shadow-[inset_2px_0_0_hsl(var(--primary))]")
  })
})
```

- [ ] **Step 2: Run them to see them fail**

Run: `cd web && timeout 300 npx vitest run src/components/page/run-row.test.tsx src/components/page/side-layout.test.tsx`
Expected: FAIL, cannot resolve `@/lib/run-cols`.

- [ ] **Step 3: Write the two small modules**

They live outside the component files because the repository's lint rule `react-refresh/only-export-components` allows a component file to export components only.

`web/src/lib/run-cols.ts`:

```ts
// Columns of a RunRow: icon and main, plus the values when wide, plus actions.
export function runCols(wide: boolean, actions: boolean): number {
  return 2 + (wide ? 1 : 0) + (actions ? 1 : 0)
}
```

`web/src/lib/side-link.ts`:

```ts
// The router sets aria-current="page" on the current link, so the style follows it.
export const SIDE_LINK =
  "flex items-center gap-2 rounded-md px-3 py-1.5 text-sm hover:bg-muted aria-[current=page]:bg-muted aria-[current=page]:font-semibold aria-[current=page]:shadow-[inset_2px_0_0_hsl(var(--primary))]"
```

- [ ] **Step 4: Write `web/src/components/page/run-row.tsx`**

```tsx
import { Calendar, Clock } from "lucide-react"
import type { ReactNode } from "react"

export function RunValue({ kind, children }: { kind: "time" | "duration"; children: ReactNode }) {
  const Icon = kind === "time" ? Calendar : Clock
  return (
    <span className="flex items-center gap-1.5 font-mono text-xs text-muted-foreground">
      <Icon size={13} aria-hidden="true" className="shrink-0" />
      {children}
    </span>
  )
}

export function RunGroup({ label, cols }: { label: string; cols: number }) {
  return (
    <tr className="border-b border-border bg-background">
      <th scope="rowgroup" colSpan={cols} className="px-4 py-1.5 text-left text-xs font-semibold text-muted-foreground">
        {label}
      </th>
    </tr>
  )
}

export function RunRow({
  icon,
  title,
  facts,
  values,
  actions,
  wide,
}: {
  icon: ReactNode
  title: ReactNode
  facts?: ReactNode
  values?: ReactNode
  actions?: ReactNode
  wide: boolean
}) {
  return (
    <tr className="border-b border-border last:border-b-0 hover:bg-muted/50">
      <td className="w-9 py-2.5 pr-0 pl-4 align-top">
        <span className="flex h-5 items-center">{icon}</span>
      </td>
      <td className="px-3 py-2.5 align-top">
        <span className="block text-sm font-semibold">{title}</span>
        {facts && <span className="mt-1 flex flex-wrap items-center gap-x-3 gap-y-0.5 text-xs text-muted-foreground">{facts}</span>}
        {!wide && values && <span className="mt-1 flex flex-wrap gap-x-3 gap-y-0.5">{values}</span>}
      </td>
      {wide && (
        <td className="w-44 py-2.5 pr-4 text-right align-top">
          <span className="flex flex-col items-end gap-1">{values}</span>
        </td>
      )}
      {actions && <td className="w-20 py-2.5 pr-3 text-right align-top">{actions}</td>}
    </tr>
  )
}
```

- [ ] **Step 5: Write `web/src/components/page/side-layout.tsx`**

```tsx
import type { ReactNode } from "react"

export function SideGroup({ children }: { children: ReactNode }) {
  return <p className="mt-3 border-t border-border px-3 pt-3 pb-1 text-xs font-semibold text-muted-foreground">{children}</p>
}

// A 240px list of links beside the content; below 800px the list stacks above it.
export function SideLayout({ label, nav, children }: { label: string; nav: ReactNode; children: ReactNode }) {
  return (
    <div className="grid gap-6 min-[800px]:grid-cols-[15rem_minmax(0,1fr)] min-[800px]:items-start">
      <nav aria-label={label} className="flex flex-col gap-0.5 min-[800px]:sticky min-[800px]:top-4">
        {nav}
      </nav>
      <div className="flex min-w-0 flex-col gap-4">{children}</div>
    </div>
  )
}
```

- [ ] **Step 6: Run the tests**

Run: `cd web && timeout 300 npx vitest run src/components/page/run-row.test.tsx src/components/page/side-layout.test.tsx && timeout 300 npm run lint && timeout 300 npm run typecheck`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add web/src/lib/run-cols.ts web/src/lib/side-link.ts web/src/components/page/run-row.tsx web/src/components/page/run-row.test.tsx web/src/components/page/side-layout.tsx web/src/components/page/side-layout.test.tsx
git commit -m "feat(web): add the run row and the side layout"
```

### Task 9: Section routes

**Files:**
- Modify: `web/src/router.tsx` (after `settingsRoute`, and the `appRoute.addChildren` list)
- Modify: `web/src/pages/repository.tsx` (`RepositoryPage` only)
- Test: `web/src/router.test.tsx`

**Interfaces:**
- Consumes: nothing.
- Produces: Contracts, "Routes".

**Items:** 7, 8

**Depends on:** none

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 1 = 4

- [ ] **Step 1: Add the failing tests to `web/src/router.test.tsx`**

Inside `describe("routes", …)`. The file already imports `mockApi`, `authedRoutes`, `renderApp` and `screen`; add any that are missing.

```tsx
  it("serves a settings section under /settings", async () => {
    mockApi(authedRoutes())
    const { router } = renderApp("/settings/token")
    expect(await screen.findByRole("heading", { level: 1, name: "Settings" })).toBeInTheDocument()
    expect(router.state.location.pathname).toBe("/settings/token")
    expect(router.state.matches.at(-1)?.routeId).toBe("/app/settings/$section")
  })

  it("serves a repository section under its repository", async () => {
    mockApi(authedRoutes())
    const { router } = renderApp("/repositories/darkmem/settings")
    expect(await screen.findByRole("heading", { level: 1, name: "darkmem" })).toBeInTheDocument()
    expect(router.state.matches.at(-1)?.routeId).toBe("/app/repositories/$name/$section")
  })
```

- [ ] **Step 2: Run it to see it fail**

Run: `cd web && timeout 300 npx vitest run src/router.test.tsx`
Expected: FAIL on both: the paths match no route.

- [ ] **Step 3: Add the two routes in `web/src/router.tsx`**

After `const settingsRoute = …`:

```ts
// The section pages read $section themselves, so the route table does not
// change when a page gains or loses a section.
const settingsSectionRoute = createRoute({ getParentRoute: () => appRoute, path: "/settings/$section", component: SettingsPage })
const repositorySectionRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/repositories/$name/$section",
  component: RepositoryPage,
})
```

Add `repositorySectionRoute` after `repositoryRoute` and `settingsSectionRoute` after `settingsRoute` in `appRoute.addChildren([...])`.

- [ ] **Step 4: Let `RepositoryPage` render under either route**

In `web/src/pages/repository.tsx`, `useParams({ from: "/app/repositories/$name" })` throws under the new route. Replace `RepositoryPage` with:

```tsx
export function RepositoryPage() {
  const { name = "" } = useParams({ strict: false })
  // Keyed by name: each repository starts with its own empty draft.
  return <RepositoryForm key={name} name={name} />
}
```

- [ ] **Step 5: Run the checks**

Run: `cd web && timeout 300 npx vitest run src/router.test.tsx src/pages/repository.test.tsx && timeout 300 npm run typecheck`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add web/src/router.tsx web/src/router.test.tsx web/src/pages/repository.tsx
git commit -m "feat(web): route settings and repository sections"
```

### Task 10: Dashboard

**Files:**
- Modify: `web/src/components/stat-card.tsx` (`StatCard`, `StatCards`)
- Modify: `web/src/components/repo-table.tsx` (`STATE_CLASS` and the state cell)
- Modify: `web/src/components/activity-panel.tsx` (the `<section>` wrapper, its header, `Legend`)
- Modify: `web/src/components/lanes-chart.tsx` (lines 14-16, 240, 242), `web/src/components/buckets-chart.tsx` (line 192)
- Test: `web/src/components/stat-card.test.tsx`, `web/src/components/repo-table.test.tsx`, `web/src/components/activity-panel.test.tsx`, `web/src/components/lanes-chart.test.tsx`, `web/src/components/buckets-chart.test.tsx`

**Interfaces:**
- Consumes: `StatusPill` (Contracts, "Status"); the `running` colour (Contracts, "Tokens and classes").
- Produces: nothing other tasks use.

**Items:** 1

**Depends on:** Task 4, Task 5

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 1 = 3

- [ ] **Step 1: Add the failing tests**

In `web/src/components/stat-card.test.tsx` (it already imports `render`, `screen`, `within`, `fixtures`, and has `now`):

```tsx
  it("holds the four stats in one box split by dividers", () => {
    const { container } = render(<StatCards status={fixtures.status} metrics={fixtures.metrics} now={now} />)
    const box = container.firstElementChild as HTMLElement
    expect(box).toHaveClass("rounded-md", "border", "bg-card", "min-[641px]:divide-x")
    const stats = within(box).getAllByRole("region")
    expect(stats.map((s) => s.getAttribute("aria-label"))).toEqual(["Runners", "Waiting jobs", "API budget", "Host"])
    for (const stat of stats) expect(stat).not.toHaveClass("border")
  })
```

In `web/src/components/activity-panel.test.tsx`, with the props its other tests pass to `ActivityPanel`:

```tsx
  it("draws Activity as a box with a header band", () => {
    draw()
    const heading = screen.getByRole("heading", { level: 2, name: "Activity" })
    expect(heading.parentElement).toHaveClass("bg-background", "border-b")
    expect(heading.closest("section")).toHaveClass("rounded-md", "overflow-hidden")
    expect(heading.closest("section")).not.toHaveClass("p-4")
  })
```

`draw` is the file's own render helper (`activity-panel.test.tsx:10`); pass it the arguments its neighbouring tests pass.

Run: `cd web && timeout 300 npx vitest run src/components/stat-card.test.tsx src/components/activity-panel.test.tsx`
Expected: FAIL on both new tests.

- [ ] **Step 2: One box for the stats, in `web/src/components/stat-card.tsx`**

Replace `StatCard`'s `<section>` opening tag and the `StatCards` wrapper's opening tag:

```tsx
    <section aria-label={label} className="flex min-w-0 flex-col gap-1 p-3">
```

```tsx
    <div className="grid grid-cols-2 divide-border rounded-md border border-border bg-card min-[641px]:grid-cols-4 min-[641px]:divide-x">
```

In `StatCard`, change the value's `font-medium` to `font-semibold`. Everything inside the four `StatCard` uses stays.

- [ ] **Step 3: Pills in the repository table, in `web/src/components/repo-table.tsx`**

Delete `STATE_CLASS`. Replace the state cell

```tsx
              <TableCell className={STATE_CLASS[word] ?? ""}>{word}</TableCell>
```

with

```tsx
              <TableCell>
                <StatusPill state={word.toLowerCase()} />
              </TableCell>
```

and add `import { StatusPill } from "@/components/page/status-pill"`. In `web/src/components/repo-table.test.tsx`, where a test asserts a colour class on the state cell (`text-primary`, `text-destructive`, `text-muted-foreground`), assert the word's text instead; the word is unchanged.

- [ ] **Step 4: The Activity box, in `web/src/components/activity-panel.tsx`**

Replace the `<section …>` opening tag and the header `<div>` that holds the `h2`, and wrap everything after the header in a padded body:

```tsx
    <section aria-labelledby="activity-title" className="min-w-0 overflow-hidden rounded-md border border-border bg-card">
      <div className="flex flex-wrap items-center gap-x-3 gap-y-2 border-b border-border bg-background px-4 py-2.5">
        <h2 id="activity-title" className="text-sm font-semibold">
```

The header's other children (the range text and the `WindowControl`) stay inside that `<div>`. Directly after the header's closing `</div>`, open `<div className="p-4">` and close it just before `</section>`, so the alert, the legend, the chart and the hidden table sit inside it.

In `Legend`, replace these lane entries:

```tsx
        ["Running", <Swatch className="fill-running stroke-running" />],
        ["Finishing", <Swatch className="fill-running/25 stroke-running/60" />],
        ["Warm", <Swatch className="fill-none stroke-running" dash />],
```

```tsx
        ["CPU", <LineSwatch className="stroke-foreground" />],
```

and this bucket entry:

```tsx
        ["Average CPU", <LineSwatch className="stroke-foreground" />],
```

"Jobs waiting" and "Most jobs waiting" keep `warning`: waiting is a state. "Busy runner-minutes" and "Busy slot time" keep `primary`.

- [ ] **Step 5: The same colours in the charts**

`web/src/components/lanes-chart.tsx`, the three state classes:

```tsx
  warm: "fill-none stroke-running",
  running: "fill-running stroke-running",
  finishing: "fill-running/25 stroke-running/60",
```

and the CPU series: on the `polyline` with `data-track="cpu"` change `stroke-primary` to `stroke-foreground`, and on the `circle` with `data-track="cpu"` change `fill-primary` to `fill-foreground`. The `data-now` line and the label class `fill-primary-foreground` stay.

`web/src/components/buckets-chart.tsx`, the CPU line only:

```tsx
          <Line runs={cpuRuns} track="cpu" stroke="stroke-foreground" fill="fill-foreground" />
```

The busy bars, the picked outline and the `data-now` line stay on `primary`.

In `lanes-chart.test.tsx`, `buckets-chart.test.tsx` and `activity-panel.test.tsx`, update each assertion that names one of the classes changed above to the new class.

- [ ] **Step 6: Run the checks**

Run: `cd web && timeout 600 npm test && timeout 300 npm run typecheck && timeout 300 npm run lint`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add web/src/components/stat-card.tsx web/src/components/stat-card.test.tsx web/src/components/repo-table.tsx web/src/components/repo-table.test.tsx web/src/components/activity-panel.tsx web/src/components/activity-panel.test.tsx web/src/components/lanes-chart.tsx web/src/components/lanes-chart.test.tsx web/src/components/buckets-chart.tsx web/src/components/buckets-chart.test.tsx
git commit -m "feat(web): redraw the Dashboard in Forge"
```

### Task 11: Runners

**Files:**
- Modify: `web/src/components/runner-list.tsx` (`LIGHT`, the row class)
- Test: `web/src/components/runner-list.test.tsx`

**Interfaces:**
- Consumes: the `running` colour (Contracts, "Tokens and classes").
- Produces: nothing other tasks use.

**Items:** 1

**Depends on:** Task 2

**Implementer:** dr-superpowers:impl-sonnet-low
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 0 = 2

- [ ] **Step 1: Add the failing test to `web/src/components/runner-list.test.tsx`**

The file has `draw(status?, path = "/runners/aaaaaa?tab=steps")` and `link(id)`; the default path selects runner `aaaaaa`.

```tsx
  it("marks the selected runner with the sunk fill and an accent edge", async () => {
    draw()
    const current = await link("aaaaaa")
    expect(current).toHaveClass("bg-muted", "shadow-[inset_2px_0_0_hsl(var(--primary))]")
    const other = fixtures.status.instances.find((i) => i.id !== "aaaaaa")
    if (other) expect(await link(other.id)).not.toHaveClass("shadow-[inset_2px_0_0_hsl(var(--primary))]")
  })
```

Run: `cd web && timeout 300 npx vitest run src/components/runner-list.test.tsx`
Expected: FAIL: the selected link lacks the `shadow-[…]` class. If the class sits on a child of the link in this file, assert on the element that carries `bg-muted`.

- [ ] **Step 2: Change `web/src/components/runner-list.tsx`**

```tsx
const LIGHT: Record<string, string> = {
  busy: "ghr-pulse bg-running",
  starting: "bg-running/45",
  idle: "border border-running",
}
```

In the row's class, replace `i.id === selected ? "bg-muted" : ""` with:

```tsx
                  i.id === selected ? "bg-muted shadow-[inset_2px_0_0_hsl(var(--primary))]" : ""
```

- [ ] **Step 3: Run the tests**

Run: `cd web && timeout 300 npx vitest run src/components/runner-list.test.tsx src/pages/runners.test.tsx`
Expected: PASS. An assertion expecting `bg-primary` or `border-primary` on a runner's light now expects `bg-running` or `border-running`.

- [ ] **Step 4: Commit**

```bash
git add web/src/components/runner-list.tsx web/src/components/runner-list.test.tsx
git add -u web/src/pages/runners.test.tsx
git commit -m "feat(web): redraw the runner list in Forge"
```

### Task 12: Actions

**Files:**
- Modify: `web/src/pages/actions.tsx` (`RunRow`, `RunHead`, the `body` branches, the filter row)
- Test: `web/src/pages/actions.test.tsx`

**Interfaces:**
- Consumes: `RunRow`, `RunValue`, `RunGroup`, `runCols` (Contracts, "Rows and side layout"); `Section` with `flush` (Contracts, "Box").
- Produces: nothing other tasks use.

**Items:** 5

**Depends on:** Task 5, Task 8

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 1 - coupling 1 - risk 1 = 4

- [ ] **Step 1: Add the failing tests to `web/src/pages/actions.test.tsx`**

The file's tests start with `mockApi(authedRoutes())` and `renderApp("/actions")`. Add `setViewport` from `@/test/media` and `fixtures` from `@/test/fixtures` to the imports if they are missing.

```tsx
  it("lists every run in one box whose header counts them and holds the filters", async () => {
    setViewport(1024)
    mockApi(authedRoutes())
    renderApp("/actions")
    const total = fixtures.actions.runs.length
    const box = await screen.findByRole("region", { name: `${total} workflow runs` })
    expect(within(box).getByRole("combobox", { name: "Repository" })).toBeInTheDocument()
    expect(within(box).getByRole("radiogroup", { name: "Status" })).toBeInTheDocument()
    expect(within(box).getAllByRole("table")).toHaveLength(1)
    expect(screen.queryByRole("region", { name: "Recent" })).not.toBeInTheDocument()
    expect(screen.queryByRole("region", { name: "In progress" })).not.toBeInTheDocument()
  })

  it("counts only the runs the filters leave", async () => {
    setViewport(1024)
    mockApi(authedRoutes())
    const repo = fixtures.actions.runs[0]!.repo
    const left = fixtures.actions.runs.filter((r) => r.repo === repo).length
    renderApp(`/actions?repo=${repo}`)
    expect(await screen.findByRole("region", { name: `${left} workflow ${left === 1 ? "run" : "runs"}` })).toBeInTheDocument()
  })

  it("heads the groups inside the box with row headers", async () => {
    setViewport(1024)
    mockApi(authedRoutes())
    renderApp("/actions")
    const box = await screen.findByRole("region", { name: /workflow runs?$/ })
    const headings = within(box).getAllByRole("rowheader")
    expect(headings.length).toBeGreaterThan(0)
    for (const h of headings) expect(h).toHaveAttribute("scope", "rowgroup")
    const active = fixtures.actions.runs.some((r) => r.status !== "completed")
    expect(headings[0]).toHaveTextContent(active ? "In progress" : /.+/)
  })

  it("writes a run's facts and values in the row", async () => {
    setViewport(1024)
    mockApi(authedRoutes())
    renderApp("/actions")
    const run = fixtures.actions.runs.find((r) => r.branch && r.actor && r.event)!
    const link = (await screen.findAllByRole("link", { name: new RegExp(`^${run.title}`) }))[0]!
    const row = within(link.closest("tr") as HTMLElement)
    expect(row.getByText(`${run.workflow} #${run.run_number}`)).toBeInTheDocument()
    expect(row.getByText(run.repo)).toBeInTheDocument()
    expect(row.getByText(`${run.event} by ${run.actor}`)).toBeInTheDocument()
    expect(row.getByText(run.branch)).toHaveClass("font-mono", "text-primary", "rounded-full")
    expect(row.queryAllByText("ghr")).toHaveLength(run.ghr ? 1 : 0)
    expect(row.getAllByRole("cell")).toHaveLength(3)
  })
```

If a run title contains characters that are special in a regular expression, escape them before building the `RegExp`.

Run: `cd web && timeout 300 npx vitest run src/pages/actions.test.tsx`
Expected: FAIL: no region named "… workflow runs".

- [ ] **Step 2: Replace the row and delete `RunHead` in `web/src/pages/actions.tsx`**

Rename the local `RunRow` to `ActionRow` and write it on the shared row. Import `RunGroup, RunRow, RunValue` from `@/components/page/run-row` and `runCols` from `@/lib/run-cols`, and drop the `Table*` imports that become unused.

```tsx
const BRANCH = "rounded-full bg-primary/10 px-2 py-0.5 font-mono text-xs text-primary"
const GHR = "rounded-full bg-muted px-2 py-0.5 font-mono text-xs text-foreground"

function ActionRow({ run, now, wide }: { run: ActionsRun; now: number; wide: boolean }) {
  const by = [run.event, run.actor && `by ${run.actor}`].filter(Boolean).join(" ")
  return (
    <RunRow
      wide={wide}
      icon={<RunStatusIcon status={run.status} conclusion={run.conclusion} />}
      title={
        <Link to="/actions/$repo/runs/$id" params={{ repo: run.repo, id: String(run.id) }} className="hover:text-primary hover:underline">
          {run.title}
          <span className="sr-only">{`, ${run.repo} #${run.run_number}`}</span>
        </Link>
      }
      facts={
        <>
          <span>{`${run.workflow} #${run.run_number}`}</span>
          <span>{run.repo}</span>
          {by && <span>{by}</span>}
          {run.branch && <span className={BRANCH}>{run.branch}</span>}
          {run.ghr && <span className={GHR}>ghr</span>}
        </>
      }
      values={
        <>
          <RunValue kind="time">{startText(run.started_at, now)}</RunValue>
          <RunValue kind="duration">{dur(runMs(run, now))}</RunValue>
        </>
      }
    />
  )
}
```

- [ ] **Step 3: One box for the lists**

Move the `Select` and the `ToggleGroup` out of the standalone filter `<div>` into a constant declared before `let body`, and add a flag beside it:

```tsx
  const filters = (
    <>
      {/* the existing <Select …>…</Select> block, unchanged */}
      {/* the existing <ToggleGroup …>…</ToggleGroup> block, unchanged */}
    </>
  )
  let listed = false
```

Render the lists as one flush section. Replace the final `else` branch of `body` with the block below, and set `listed = true` as that branch's first statement:

```tsx
    const shown = active.length + recent.length
    const cols = runCols(wide, false)
    body = (
      <Section title={`${shown} workflow ${shown === 1 ? "run" : "runs"}`} aside={<div className="ml-auto flex flex-wrap items-center gap-3">{filters}</div>} flush>
        <div className="overflow-x-auto">
          <table className="w-full border-collapse">
            {(active.length > 0 || status === "active") && (
              <tbody>
                <RunGroup label="In progress" cols={cols} />
                {active.length === 0 ? (
                  <tr>
                    <td colSpan={cols} className="px-4 py-3 text-sm text-muted-foreground">
                      Nothing is running or queued.
                    </td>
                  </tr>
                ) : (
                  active.map((r) => <ActionRow key={`${r.repo}#${r.id}`} run={r} now={now} wide={wide} />)
                )}
              </tbody>
            )}
            {groupByDayOf(recent, (r) => r.started_at, now).map((g) => (
              <tbody key={g.key}>
                <RunGroup label={g.label} cols={cols} />
                {g.rows.map((r) => (
                  <ActionRow key={`${r.repo}#${r.id}`} run={r} now={now} wide={wide} />
                ))}
              </tbody>
            ))}
          </table>
        </div>
      </Section>
    )
```

Delete the old `const cols = wide ? 5 : 2`. In the page's return, render `filters` in the standalone row only when `body` is not that section: keep a boolean `listed` set to `true` in the final branch, and render `{!listed && <div className="flex flex-wrap items-center gap-3">{filters}</div>}` where the filter row was. Every other branch of `body`, the header, the alert and the error line stay as they are.

- [ ] **Step 4: Run the tests**

Run: `cd web && timeout 300 npx vitest run src/pages/actions.test.tsx && timeout 300 npm run typecheck && timeout 300 npm run lint`
Expected: failures only in older tests that use `region("In progress")` or `region("Recent")`, or look for the column headers "Run", "Duration" or "Started". Point each at the single region (`screen.getByRole("region", { name: /workflow runs?$/ })`) and at the row headers "In progress" and the day labels; delete the column-header assertions; an icon a test finds by the name "Queued" is named "Waiting". Re-run until all pass, then run `timeout 300 npx vitest run src/a11y.test.tsx`.

- [ ] **Step 5: Commit**

```bash
git add web/src/pages/actions.tsx web/src/pages/actions.test.tsx
git commit -m "feat(web): list workflow runs as rows in one box"
```

### Task 13: History

**Files:**
- Modify: `web/src/pages/history.tsx` (`HistoryRow`, the table branch of `body`, the "Finished jobs" section, the `PageHeader`)
- Test: `web/src/pages/history.test.tsx`

**Interfaces:**
- Consumes: `RunRow`, `RunValue`, `RunGroup`, `runCols` (Contracts, "Rows and side layout"); `Section` with `flush` (Contracts, "Box").
- Produces: nothing other tasks use.

**Items:** 5

**Depends on:** Task 5, Task 8

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 1 - coupling 1 - risk 1 = 4

- [ ] **Step 1: Add the failing tests to `web/src/pages/history.test.tsx`**

The file's tests start with `mockApi(authedRoutes({ "GET /api/history": fixtures.history }))` and `renderApp("/history")`. Add `setViewport` from `@/test/media` to the imports if it is missing.

```tsx
  it("links a finished job to its run page and lists its facts", async () => {
    setViewport(1024)
    mockApi(authedRoutes({ "GET /api/history": fixtures.history }))
    renderApp("/history")
    const entry = fixtures.history.find((h) => h.run_id > 0)!
    const link = (await screen.findAllByRole("link", { name: entry.job_name }))[0]!
    expect(link).toHaveAttribute("href", `/actions/${entry.repo}/runs/${entry.run_id}`)
    const row = link.closest("tr") as HTMLElement
    expect(within(row).getByText(entry.workflow)).toBeInTheDocument()
    expect(within(row).getByText(entry.repo)).toBeInTheDocument()
    expect(within(row).getByText(`#${entry.run_number}`)).toBeInTheDocument()
    expect(within(row).getByRole("img", { name: resultWord(entry.conclusion) })).toBeInTheDocument()
  })

  it("puts the summary in the list's header band and the duration bar under the duration", async () => {
    setViewport(1024)
    mockApi(authedRoutes({ "GET /api/history": fixtures.history }))
    renderApp("/history")
    const entry = fixtures.history[0]!
    await screen.findAllByRole("link", { name: entry.job_name })
    const box = screen.getByRole("region", { name: "Finished jobs" })
    const heading = within(box).getByRole("heading", { level: 2, name: "Finished jobs" })
    expect(heading.parentElement).toHaveTextContent(/\d+ jobs?/)
    expect(within(box).queryByRole("columnheader")).not.toBeInTheDocument()
    const bar = box.querySelector("[data-duration-bar]") as HTMLElement
    expect(bar.parentElement).toHaveClass("flex-col")
  })
```

Import `resultWord` from `@/lib/history`.

Run: `cd web && timeout 300 npx vitest run src/pages/history.test.tsx`
Expected: FAIL: no link named after the job.

- [ ] **Step 2: Rewrite `HistoryRow` in `web/src/pages/history.tsx`**

Import `Link` from `@tanstack/react-router` if it is not imported, `RunGroup, RunRow, RunValue` from `@/components/page/run-row`, `runCols` from `@/lib/run-cols`, and `useMediaQuery` from `@/lib/use-media-query`.

```tsx
function HistoryRow({ entry, longest, wide }: { entry: HistoryEntry; longest: number; wide: boolean }) {
  const ms = took(entry)
  const url = entry.html_url
  const name = `${entry.job_name} #${entry.run_number}`
  return (
    <RunRow
      wide={wide}
      icon={<ResultIcon conclusion={entry.conclusion} />}
      title={
        entry.run_id > 0 ? (
          <Link to="/actions/$repo/runs/$id" params={{ repo: entry.repo, id: String(entry.run_id) }} className="hover:text-primary hover:underline">
            {entry.job_name}
          </Link>
        ) : (
          entry.job_name
        )
      }
      facts={
        <>
          <span>{entry.workflow}</span>
          <span>{entry.repo}</span>
          <span className="font-mono">#{entry.run_number}</span>
        </>
      }
      values={
        <>
          <RunValue kind="time">{hhmm(new Date(entry.finished_at))}</RunValue>
          <span className="flex flex-col items-end gap-1">
            <RunValue kind="duration">{dur(ms)}</RunValue>
            <span data-duration-bar aria-hidden="true" className="block h-1.5 w-20 rounded-[2px] bg-muted">
              <span className="block h-1.5 rounded-[2px] bg-foreground/40" style={{ width: `${Math.round((ms / longest) * 100)}%` }} />
            </span>
          </span>
        </>
      }
      actions={url ? <span className="flex justify-end gap-1">{OPEN_AND_COPY}</span> : <span />}
    />
  )
}
```

`OPEN_AND_COPY` stands for the two existing `<Tooltip>…</Tooltip>` blocks of the old row's last cell ("Open run" and "Copy run URL"): move both, unchanged, into that `<span>`. They use `url` and `name`.

- [ ] **Step 3: Rewrite the table branch, the section and the header**

In `HistoryPage` add `const wide = useMediaQuery("(min-width: 768px)")`. Replace the last `else` branch of `body`:

```tsx
    const cols = runCols(wide, true)
    body = (
      <div className="overflow-x-auto">
        <table className="w-full border-collapse">
          {groups.map((g) => (
            <tbody key={g.key}>
              <RunGroup label={g.label} cols={cols} />
              {g.rows.map((h) => (
                <HistoryRow key={h.id} entry={h} longest={longest} wide={wide} />
              ))}
            </tbody>
          ))}
        </table>
      </div>
    )
```

Replace the "Finished jobs" `Section`:

```tsx
      <Section title="Finished jobs" aside={settled ? <span className="text-sm text-muted-foreground">{historySummary(rows)}</span> : undefined} flush>
        {picked && (
          <div className="flex flex-wrap items-center gap-2 border-b border-border px-4 py-2.5 text-sm">
            <span>{pickText(picked)}</span>
            <Button size="sm" variant="outline" onClick={() => setPick(null)}>
              Show whole window
            </Button>
          </div>
        )}
        {all.length >= HISTORY_LIMIT && (
          <p className="border-b border-border px-4 py-2.5 text-sm text-muted-foreground">Showing the newest 500 jobs in this window.</p>
        )}
        {settled && rows.length > 0 ? body : <div className="p-4">{body}</div>}
      </Section>
```

The summary now lives in the band, so the page header loses it: change `<PageHeader title="History" description={…} />` to `<PageHeader title="History" />`. Remove the `Table*` imports that become unused. The controls row and the "Jobs" chart section stay as they are.

- [ ] **Step 4: Run the tests and update the ones the new markup reaches**

Run: `cd web && timeout 300 npx vitest run src/pages/history.test.tsx src/a11y.test.tsx && timeout 300 npm run typecheck && timeout 300 npm run lint`
Expected: failures only in older history tests that:

- look for the column headers Finished, Repository, Job, Run, Result or Duration: delete those assertions;
- read a result as visible text in a row ("Succeeded", "Failed", "Cancelled"): read it as `getByRole("img", { name })` in that row;
- find the summary sentence in the page header: it is found once, in the "Finished jobs" region.

Day headings, the pick line, the 500-row note, the empty states and the request assertions stay unchanged. Re-run until all pass.

- [ ] **Step 5: Commit**

```bash
git add web/src/pages/history.tsx web/src/pages/history.test.tsx
git commit -m "feat(web): list finished jobs as rows"
```

### Task 14: Run page

**Files:**
- Modify: `web/src/components/run-frame.tsx` (`RAIL_LINK`, `RAIL_LABEL`, the header actions)
- Modify: `web/src/components/run-graph.tsx` (the graph canvas, connectors and tiles)
- Modify: `web/src/pages/run.tsx` (`SummaryFacts` only)
- Test: `web/src/components/run-frame.test.tsx`, `web/src/components/run-graph.test.tsx`, `web/src/pages/run.test.tsx`

**Interfaces:**
- Consumes: `StatusPill` (Contracts, "Status"); `SIDE_LINK`, `SideGroup` (Contracts, "Rows and side layout").
- Produces: nothing other tasks use.

**Items:** 6

**Depends on:** Task 4, Task 8

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 1 = 3

The kit's `PageHeader` takes `title` and `description` as strings (`web/node_modules/darkraise-ui/dist/layout/types.d.ts:56-62`), so the run's status pill goes first in the header's actions row.

- [ ] **Step 1: Add the failing tests**

In `web/src/components/run-frame.test.tsx` (it has `renderFrame(over?, run?)`):

```tsx
  it.each([
    [{ status: "completed", conclusion: "failure" }, "Failed"],
    [{ status: "completed", conclusion: "success" }, "Succeeded"],
    [{ status: "in_progress", conclusion: "" }, "Running"],
    [{ status: "queued", conclusion: "" }, "Waiting"],
  ])("shows the run's state as a pill before the header actions (%o)", (over, word) => {
    renderFrame({}, { ...fixtures.runDetail, ...over })
    const github = screen.getByRole("link", { name: "Open on GitHub" })
    const actions = github.closest("div") as HTMLElement
    const pill = within(actions).getByText(word)
    expect(pill.parentElement).toHaveClass("rounded-full")
    expect(actions.firstElementChild).toContainElement(pill)
  })

  it("marks the current entry of the job list with the side-link style", () => {
    renderFrame()
    const summary = screen.getByRole("link", { name: "Summary" })
    expect(summary.className).toContain("aria-[current=page]:shadow-[inset_2px_0_0_hsl(var(--primary))]")
    expect(screen.getByText("Jobs")).toHaveClass("border-t")
  })
```

Add `within` to the file's imports if it is missing.

In `web/src/components/run-graph.test.tsx` (it has `show(graph?, jobs?)`):

```tsx
  it("draws the jobs on a ground canvas with thin muted connectors", () => {
    const { container } = show()
    expect(screen.getByRole("group", { name: "Job graph" })).toHaveClass("bg-background", "rounded-md", "p-3")
    const svg = container.querySelector("svg[aria-hidden='true']") as SVGElement
    expect(svg).toHaveClass("text-muted-foreground")
    expect(svg.querySelector("path")).toHaveAttribute("stroke-width", "1")
  })
```

If `show()` returns something other than the render result, read `container` from `document.body`.

Run: `cd web && timeout 300 npx vitest run src/components/run-frame.test.tsx src/components/run-graph.test.tsx`
Expected: FAIL on all new tests.

- [ ] **Step 2: Change `web/src/components/run-frame.tsx`**

Add the imports and the pill, replace the two class constants:

```tsx
import { StatusPill } from "@/components/page/status-pill"
import { SideGroup } from "@/components/page/side-layout"
import { SIDE_LINK } from "@/lib/side-link"
import { isRunActive } from "@/lib/run-details"

const RAIL_LINK = SIDE_LINK

function RunPill({ run }: { run: RunDetail }) {
  if (!isRunActive(run.status)) return <StatusPill state={run.conclusion === "success" || run.conclusion === "cancelled" || run.conclusion === "skipped" ? run.conclusion : "failure"} />
  return <StatusPill state={run.status === "in_progress" ? "running" : "queued"} />
}
```

Merge the `isRunActive` import into the file's existing import from `@/lib/run-details`. Delete `RAIL_LABEL` and replace each `<p className={RAIL_LABEL}>…</p>` with `<SideGroup>…</SideGroup>`.

In `actions`, put the pill first, in its own wrapper:

```tsx
  const actions = (
    <div className="flex flex-wrap items-center gap-2">
      <span className="flex items-center">
        <RunPill run={run} />
      </span>
      {/* the attempt Select block and the "Open on GitHub" Button, unchanged */}
    </div>
  )
```

The `PageHeader` call stays as it is.

- [ ] **Step 3: Change `web/src/components/run-graph.tsx`**

On the element with `role="group" aria-label="Job graph"`:

```tsx
      <div role="group" aria-label="Job graph" tabIndex={0} className="overflow-x-auto rounded-md bg-background p-3">
```

On the connectors' `<svg>`, change `text-border` to `text-muted-foreground`, and on its `<path>` change `strokeWidth={1.5}` to `strokeWidth={1}`.

On the matrix tile's wrapper, replace `rounded-[6px] border border-border bg-card` with `rounded-md border border-border bg-card hover:border-primary focus-within:border-primary`. Give the single-job tile's wrapper the same classes in place of its own radius, border and background classes. The graph is drawn on the run's summary page, where no job is selected, so there is no "current" tile.

- [ ] **Step 4: Wrap the summary pairs below 800px, in `web/src/pages/run.tsx`**

In `SummaryFacts`, change the `<dl>` class from `grid grid-cols-2 gap-4 sm:grid-cols-4` to:

```tsx
    <dl className="grid grid-cols-2 gap-4 min-[800px]:grid-cols-4">
```

Add to `web/src/pages/run.test.tsx`, in the describe block that renders the run page with `runRoutes()`:

```tsx
  it("lays the summary out as four pairs from 800px", async () => {
    mockApi(runRoutes())
    renderApp("/actions/darkmem/runs/102")
    const status = await screen.findByText("Total duration")
    const list = status.closest("dl") as HTMLElement
    expect(list).toHaveClass("grid-cols-2", "min-[800px]:grid-cols-4")
    expect(list).not.toHaveClass("sm:grid-cols-4")
  })
```

- [ ] **Step 5: Run the checks**

Run: `cd web && timeout 300 npx vitest run src/components/run-frame.test.tsx src/components/run-graph.test.tsx src/pages/run.test.tsx && timeout 300 npm run typecheck && timeout 300 npm run lint`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add web/src/components/run-frame.tsx web/src/components/run-frame.test.tsx web/src/components/run-graph.tsx web/src/components/run-graph.test.tsx web/src/pages/run.tsx web/src/pages/run.test.tsx
git commit -m "feat(web): redraw the run page in Forge"
```

### Task 15: Job log panel

**Files:**
- Modify: `web/src/components/log-viewer.tsx` (`LogViewer`'s props and returned markup, `renderLines`' wrapper)
- Modify: `web/src/pages/run.tsx` (`JobLogs`, `JobView`)
- Modify: `web/src/styles/ghr-theme.css` (after the `.ghr-log` rules)
- Test: `web/src/components/log-viewer.test.tsx`, `web/src/pages/run.test.tsx`, `web/src/styles/theme.test.ts`

**Interfaces:**
- Consumes: the `:root[data-mode="light"] .ghr-log-panel` token block (Contracts, "Shell").
- Produces: `LogViewer` gains `heading?: ReactNode`; nothing other tasks use.

**Items:** 6

**Depends on:** Task 7, Task 14

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 1 = 3

- [ ] **Step 1: Add the failing tests**

In `web/src/components/log-viewer.test.tsx` (it has `renderViewer(props?)`):

```tsx
  it("holds the heading, the search box, the toolbar and the steps in one dark panel", () => {
    renderViewer({ heading: <span>lint 1m12s</span>, toolbar: <button type="button">Raw log</button> })
    const panel = screen.getByRole("list", { name: "Steps" }).closest(".ghr-log-panel") as HTMLElement
    expect(panel).not.toBeNull()
    expect(within(panel).getByText("lint 1m12s")).toBeInTheDocument()
    expect(within(panel).getByLabelText("Search logs")).toBeInTheDocument()
    expect(within(panel).getByRole("button", { name: "Raw log" })).toBeInTheDocument()
  })
```

In `web/src/styles/theme.test.ts`, inside `describe("ghr chrome styles", …)`:

```ts
  it("paints the log panel dark and marks error and warning lines on their left edge", () => {
    expect(css).toMatch(/\.ghr-log-panel \{\s*background: hsl\(var\(--log-bg\)\);\s*color: hsl\(var\(--log-fg\)\);/)
    expect(css).toMatch(/\.ghr-log-error \{[^}]*box-shadow: inset 2px 0 0 hsl\(var\(--destructive\)\);/)
    expect(css).toMatch(/\.ghr-log-warning \{[^}]*box-shadow: inset 2px 0 0 hsl\(var\(--warning\)\);/)
  })
```

Run: `cd web && timeout 300 npx vitest run src/components/log-viewer.test.tsx src/styles/theme.test.ts`
Expected: FAIL on both new tests.

- [ ] **Step 2: Add the panel rules to `web/src/styles/ghr-theme.css`**

After the `.ghr-log { … }` rule add:

```css
.ghr-log-panel {
  background: hsl(var(--log-bg));
  color: hsl(var(--log-fg));
}
```

Inside the existing `.ghr-log-error { … }` rule add the line `box-shadow: inset 2px 0 0 hsl(var(--destructive));`, and inside the existing `.ghr-log-warning { … }` rule add `box-shadow: inset 2px 0 0 hsl(var(--warning));`.

In dark mode the page tokens are already dark. In light mode the block `:root[data-mode="light"] .ghr-log-panel` rebinds them, so kit controls inside the panel (the search input, buttons, muted text) take the dark palette.

- [ ] **Step 3: Wrap the viewer in the panel, in `web/src/components/log-viewer.tsx`**

Add `heading?: ReactNode` to `LogViewer`'s props (destructure it beside `toolbar`). Replace the returned outer `<div ref={ref} className="flex flex-col gap-3">` and the toolbar `<div className="flex flex-wrap items-center gap-3">` with:

```tsx
    <div ref={ref} className="ghr-log-panel overflow-hidden rounded-md border border-border">
      <div className="flex flex-wrap items-center gap-3 border-b border-border px-3 py-2">
        {heading && <div className="mr-auto flex min-w-0 items-center gap-2 text-sm font-semibold">{heading}</div>}
```

The search `Input`, the match readout and `{toolbar}` stay inside that toolbar `<div>`, in their current order. The `<ol aria-label="Steps">` and its closing tags stay.

In each step's button, change `flex w-full items-center gap-2 py-1.5` to `flex w-full items-center gap-2 px-3 py-1.5`. In `renderLines`, change the wrapper class from `ghr-log overflow-x-auto rounded-md py-2 font-mono text-xs leading-5` to `ghr-log overflow-x-auto py-2 font-mono text-xs leading-5`, and change the `<div className="pb-2">` around `renderLines(s)` to `<div>`.

- [ ] **Step 4: Pass the job as the heading and panel the running job, in `web/src/pages/run.tsx`**

Give `JobLogs` the duration: change its signature to `function JobLogs({ repo, job, search, ms }: { repo: string; job: RunJob; search: JobSearch; ms: number | undefined })`, and on its `<LogViewer …>` add:

```tsx
      heading={
        <>
          <RunStatusIcon status={job.status} conclusion={job.conclusion} />
          <span className="min-w-0 truncate">{job.name}</span>
          {ms !== undefined && <span className="font-mono text-xs font-normal text-muted-foreground">{dur(ms)}</span>}
        </>
      }
```

`toolbar={rawButton}` and the expired and too-large branches stay as they are.

In `JobView`, pass `ms={ms}` to `<JobLogs …>`, change the section's class from `min-w-0 rounded-[10px] border border-border bg-card p-4` to `min-w-0 rounded-md border border-border bg-card p-4`, and replace the running-job branch

```tsx
            <div className="flex flex-col gap-2">
              <p className="text-sm text-muted-foreground">Logs appear when the job finishes.</p>
              <StepList steps={job.steps} now={now} live />
            </div>
```

with

```tsx
            <div className="ghr-log-panel flex flex-col gap-2 overflow-hidden rounded-md border border-border p-3">
              <p className="text-sm text-muted-foreground">Logs appear when the job finishes.</p>
              <StepList steps={job.steps} now={now} live />
            </div>
```

Add to `web/src/pages/run.test.tsx`, beside the existing job-page tests (they render `/actions/darkmem/runs/102/jobs/1002` with `runRoutes()`):

```tsx
  it("keeps Raw log and shows the job's name in the log panel", async () => {
    mockApi(runRoutes())
    renderApp("/actions/darkmem/runs/102/jobs/1002")
    const search = await screen.findByLabelText("Search logs")
    const panel = search.closest(".ghr-log-panel") as HTMLElement
    expect(within(panel).getByRole("button", { name: "Raw log" })).toBeInTheDocument()
    const job = fixtures.runDetail.jobs.find((j) => j.id === 1002)!
    expect(within(panel).getByText(job.name)).toBeInTheDocument()
  })
```

- [ ] **Step 5: Run the checks**

Run: `cd web && timeout 300 npx vitest run src/components/log-viewer.test.tsx src/pages/run.test.tsx src/styles/theme.test.ts src/a11y.test.tsx && timeout 300 npm run typecheck && timeout 300 npm run lint`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add web/src/components/log-viewer.tsx web/src/components/log-viewer.test.tsx web/src/pages/run.tsx web/src/pages/run.test.tsx web/src/styles/ghr-theme.css web/src/styles/theme.test.ts
git commit -m "feat(web): hold a job's steps and log in one dark panel"
```

### Task 16: Repositories list

**Files:**
- Create: `web/src/components/repo-list.tsx`
- Test: `web/src/components/repo-list.test.tsx`
- Modify: `web/src/pages/repositories.tsx` (the "Configured repositories" section and the `PageHeader`), `web/src/pages/repositories.test.tsx`
- Modify: `web/src/components/repo-table.tsx` (export `RowMenu` with `onEdit`; `RepoTable` gains `onEdit` and loses `columns` and `config`), `web/src/components/repo-table.test.tsx`
- Modify: `web/src/pages/dashboard.tsx` (the `RepoTable` call), `web/src/pages/dashboard.test.tsx`

**Interfaces:**
- Consumes: `StatusPill` (Contracts, "Status"); `Section` with `flush` (Contracts, "Box"); `/repositories/$name/$section` (Contracts, "Routes").
- Produces: `RowMenu({ repo, offline, onEdit })` and `RepoTable`'s `onEdit` prop; nothing later tasks use.

**Items:** 7

**Depends on:** Task 4, Task 5, Task 9, Task 10

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 1 = 3

- [ ] **Step 1: Write the failing test `web/src/components/repo-list.test.tsx`**

```tsx
import { screen, within } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"
import type { RepoStatus } from "@/api/types"
import { mockApi } from "@/test/api"
import { fixtures } from "@/test/fixtures"
import { renderRoutes } from "@/test/routes"
import { RepoList } from "./repo-list"

function draw(repos: RepoStatus[] = fixtures.status.repos, mode = fixtures.status.mode, onEdit = vi.fn()) {
  mockApi({})
  const view = renderRoutes(
    {
      "/": () => (
        <RepoList repos={repos} config={{ ...fixtures.config, mode }} activity={fixtures.activityBuckets.repos} offline={false} onEdit={onEdit} />
      ),
      "/repositories/$name": () => <p>overview</p>,
    },
    "/",
  )
  return { ...view, onEdit }
}

const row = async (name: string) => within((await screen.findByRole("link", { name })).closest("li") as HTMLElement)

describe("RepoList", () => {
  it("shows one row per repository, with its name as a link and its state as a pill", async () => {
    draw()
    const repo = fixtures.status.repos[0]!
    const link = await screen.findByRole("link", { name: repo.name })
    expect(link).toHaveAttribute("href", `/repositories/${repo.name}`)
    expect(screen.getAllByRole("listitem")).toHaveLength(fixtures.status.repos.length)
    expect((link.closest("li") as HTMLElement).querySelector("span[class*='rounded-full']")).not.toBeNull()
  })

  it("keeps a repository's error text under its name", async () => {
    const broken = { ...fixtures.status.repos[0]!, error: "PAT cannot read this repository" }
    draw([broken])
    expect((await row(broken.name)).getByText("PAT cannot read this repository")).toHaveClass("text-destructive")
  })

  it("says a repository is being removed in place of its facts", async () => {
    const leaving = { ...fixtures.status.repos[0]!, removing: true }
    draw([leaving])
    expect((await row(leaving.name)).getByText("Removing. Running jobs finish first.")).toBeInTheDocument()
  })

  it("shows the warm count in all mode only", async () => {
    const repo = fixtures.status.repos[0]!
    const first = draw(undefined, "all")
    expect((await row(repo.name)).getByText(/^Warm \d+$/)).toBeInTheDocument()
    first.unmount()
    draw(undefined, "queue")
    expect((await row(repo.name)).queryByText(/^Warm \d+$/)).not.toBeInTheDocument()
  })

  it("sends Edit to onEdit", async () => {
    const repo = fixtures.status.repos[0]!
    const { user, onEdit } = draw()
    await user.click((await row(repo.name)).getByRole("button", { name: `More actions for ${repo.name}` }))
    await user.click(await screen.findByRole("menuitem", { name: "Edit" }))
    expect(onEdit).toHaveBeenCalledWith(repo.name)
  })

  it("stacks the right-hand group under the facts below 768px", async () => {
    draw()
    const item = (await screen.findByRole("link", { name: fixtures.status.repos[0]!.name })).closest("li") as HTMLElement
    expect(item).toHaveClass("flex-col", "md:flex-row")
  })
})
```

If `mockApi({})` is not how this repository's component tests stub an idle API, use the call `web/src/components/repo-table.test.tsx` uses.

Run: `cd web && timeout 300 npx vitest run src/components/repo-list.test.tsx`
Expected: FAIL, cannot resolve `./repo-list`.

- [ ] **Step 2: Give `RowMenu` and `RepoTable` an `onEdit`, in `web/src/components/repo-table.tsx`**

- `RowMenu`: export it, and rename its `onOpen` prop to `onEdit` (the signature becomes `export function RowMenu({ repo, offline, onEdit }: { repo: RepoStatus; offline: boolean; onEdit: (name: string) => void })`); its Edit item calls `onEdit(repo.name)`.
- `RepoTable`: add the prop `onEdit: (name: string) => void` and pass it to `<RowMenu repo={r} offline={offline} onEdit={onEdit} />`. `onOpen` keeps serving the row click and the name link.
- Delete the `columns` and `config` props, `full`, `warmShown`, `cfg`, `rate`, the Warm, 7 days and Labels header cells and body cells, the "Removing…" line guarded by `full`, the `EXTRA` constant, and the imports that become unused (`weekRate`, `weekText`, `Config`).

In `web/src/components/repo-table.test.tsx`, delete the cases that pass `columns="full"`, pass `onEdit={vi.fn()}` wherever `RepoTable` is rendered, and where a case clicks "Edit" expect `onEdit` to be called and `onOpen` not.

- [ ] **Step 3: Write `web/src/components/repo-list.tsx`**

```tsx
import { Link } from "@tanstack/react-router"
import type { ActivityRepo, Config, RepoStatus } from "@/api/types"
import { StatusPill } from "@/components/page/status-pill"
import { RepoActivityStrip } from "@/components/repo-activity-strip"
import { RowMenu } from "@/components/repo-table"
import { RunnerMeter } from "@/components/runner-meter"
import { weekRate, weekText } from "@/lib/repos"
import { repoStateWord } from "@/lib/status"

const LABEL = "rounded-full bg-muted px-2 py-0.5 font-mono text-xs text-foreground"

export function RepoList({
  repos,
  config,
  activity,
  offline,
  onEdit,
}: {
  repos: RepoStatus[]
  config: Config | undefined
  activity: ActivityRepo[] | undefined
  offline: boolean
  onEdit: (name: string) => void
}) {
  return (
    <ul className="flex flex-col divide-y divide-border">
      {repos.map((r) => {
        const seen = activity?.find((a) => a.repo.toLowerCase() === r.name.toLowerCase())
        const cfg = config?.repos?.find((c) => c.name === r.name)
        const rate = weekRate(seen?.week)
        return (
          <li key={r.name} className="flex flex-col gap-2 px-4 py-3 hover:bg-muted/50 md:flex-row md:items-start md:gap-6">
            <div className="flex min-w-0 flex-1 flex-col gap-1.5">
              <span className="flex flex-wrap items-center gap-2">
                <Link to="/repositories/$name" params={{ name: r.name }} className="text-sm font-semibold break-all text-primary hover:underline">
                  {r.name}
                </Link>
                <StatusPill state={repoStateWord(r).toLowerCase()} />
              </span>
              {r.error && <p className="text-xs text-destructive">{r.error}</p>}
              {r.removing ? (
                <p className="text-xs text-muted-foreground">Removing. Running jobs finish first.</p>
              ) : (
                <span className="flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-muted-foreground">
                  {(cfg?.labels ?? []).map((l) => (
                    <span key={l} className={LABEL}>
                      {l}
                    </span>
                  ))}
                  {config?.mode === "all" && cfg && <span>{`Warm ${cfg.warm ?? 1}`}</span>}
                  {rate !== undefined && seen && (
                    <span>
                      <span className="font-mono">{`${rate}%`}</span> succeeded in 7 days
                      <span className="sr-only">{` ${weekText(seen.week)}`}</span>
                    </span>
                  )}
                </span>
              )}
            </div>
            <div className="flex w-full flex-wrap items-center gap-4 md:w-auto md:flex-nowrap">
              <span className="flex items-center gap-2 font-mono text-sm">
                {r.max === 0 ? r.active : `${r.active}/${r.max}`}
                {r.max > 0 && <RunnerMeter active={r.active} max={r.max} />}
              </span>
              {r.queued > 0 && <span className="font-mono text-sm text-warning">{`${r.queued} waiting`}</span>}
              {seen && (
                <span className="hidden md:block">
                  <RepoActivityStrip repo={r.name} hours={seen.hours} />
                </span>
              )}
              <span className="ml-auto md:ml-0">
                <RowMenu repo={r} offline={offline} onEdit={onEdit} />
              </span>
            </div>
          </li>
        )
      })}
    </ul>
  )
}
```

- [ ] **Step 4: Use the list on the Repositories page**

In `web/src/pages/repositories.tsx`, replace the `<Section title="Configured repositories">…</Section>` block with:

```tsx
        <Section
          title="Configured repositories"
          aside={
            <>
              <span className="text-sm text-muted-foreground">{reposSummary(st)}</span>
              <span className="ml-auto">{add}</span>
            </>
          }
          flush
        >
          <RepoList
            repos={st.repos}
            config={config.data}
            activity={activity.data?.repos}
            offline={offline}
            onEdit={(name) => void navigate({ to: "/repositories/$name/$section", params: { name, section: "settings" } })}
          />
        </Section>
```

Change the page header to `<PageHeader title="Repositories" />` when the list is shown; the empty state ("No repositories yet") keeps its own Add button, and its header keeps `actions={add}`. Replace the `RepoTable` import with `import { RepoList } from "@/components/repo-list"`, and drop `useNow` and `now` if nothing else on the page uses them.

In `web/src/pages/repositories.test.tsx`: change the `row` helper to `closest("li")`; replace assertions on the column headers "Warm", "7 days" and "Labels" with assertions on the row's text ("Warm 1", "50%" and each label); and where a test finds the summary sentence or the Add button in the page header, find it in the region "Configured repositories". Other assertions stay.

- [ ] **Step 5: Send the Dashboard's Edit to the settings section**

In `web/src/pages/dashboard.tsx`, add to the `<RepoTable …>` call:

```tsx
                onEdit={(name) => void navigate({ to: "/repositories/$name/$section", params: { name, section: "settings" } })}
```

Add to `web/src/pages/dashboard.test.tsx`:

```tsx
  it("opens a repository's settings section from Edit", async () => {
    mockApi(authedRoutes())
    const { user, router } = renderApp("/")
    const name = fixtures.status.repos[0]!.name
    await user.click(await screen.findByRole("button", { name: `More actions for ${name}` }))
    await user.click(await screen.findByRole("menuitem", { name: "Edit" }))
    await waitFor(() => expect(router.state.location.pathname).toBe(`/repositories/${name}/settings`))
  })
```

Add `waitFor` and `fixtures` to that file's imports if they are missing.

- [ ] **Step 6: Run the checks**

Run: `cd web && timeout 300 npx vitest run src/components/repo-list.test.tsx src/components/repo-table.test.tsx src/pages/repositories.test.tsx src/pages/dashboard.test.tsx src/a11y.test.tsx && timeout 300 npm run typecheck && timeout 300 npm run lint`
Expected: PASS. `src/a11y.test.tsx` still finds the region "Configured repositories" and the text "50%".

- [ ] **Step 7: Commit**

```bash
git add web/src/components/repo-list.tsx web/src/components/repo-list.test.tsx web/src/pages/repositories.tsx web/src/pages/repositories.test.tsx web/src/components/repo-table.tsx web/src/components/repo-table.test.tsx web/src/pages/dashboard.tsx web/src/pages/dashboard.test.tsx
git commit -m "feat(web): list repositories as rows"
```

### Task 17: Repository page sections

**Files:**
- Modify: `web/src/pages/repository.tsx` (`RepositoryPage`, `SECTIONS`, `HeaderActions`, the returned layout of `RepositoryForm`)
- Test: `web/src/pages/repository.test.tsx`, `web/src/a11y.test.tsx`

**Interfaces:**
- Consumes: `/repositories/$name/$section` (Contracts, "Routes"); `SideLayout`, `SIDE_LINK` (Contracts, "Rows and side layout"); `StatusPill` (Contracts, "Status").
- Produces: nothing other tasks use.

**Items:** 7

**Depends on:** Task 4, Task 8, Task 9

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 2 = 4

The kit's `PageHeader` takes `description` as a string, so the repository's state pill goes first in the header's actions row.

- [ ] **Step 1: Add the failing tests to `web/src/pages/repository.test.tsx`**

The file has `routes(over?)` and renders with `mockApi(routes())` and `renderApp("/repositories/darkmem")`.

```tsx
  it("opens on the overview and keeps the form on its own section", async () => {
    mockApi(routes())
    renderApp("/repositories/darkmem")
    const nav = await screen.findByRole("navigation", { name: "Repository" })
    expect(within(nav).getAllByRole("link").map((a) => a.textContent)).toEqual([
      "Overview",
      "Capacity and labels",
      "Workflow labels",
      "GitHub registrations",
    ])
    expect(within(nav).getByRole("link", { name: "Overview" })).toHaveAttribute("aria-current", "page")
    expect(await screen.findByRole("group", { name: "Jobs per bucket for the last 24 hours" })).toBeInTheDocument()
    expect(screen.queryByLabelText("Max")).not.toBeInTheDocument()
  })

  it.each([
    ["settings", "Capacity and labels"],
    ["labels", "Workflow labels"],
    ["registrations", "GitHub registrations"],
  ])("shows the %s section at its own address", async (section, link) => {
    mockApi(routes())
    renderApp(`/repositories/darkmem/${section}`)
    const nav = await screen.findByRole("navigation", { name: "Repository" })
    expect(within(nav).getByRole("link", { name: link })).toHaveAttribute("aria-current", "page")
    expect(within(nav).getByRole("link", { name: "Overview" })).not.toHaveAttribute("aria-current")
    expect(screen.queryByRole("group", { name: "Jobs per bucket for the last 24 hours" })).not.toBeInTheDocument()
  })

  it("shows the capacity form on the settings section only", async () => {
    mockApi(routes())
    renderApp("/repositories/darkmem/settings")
    expect(await screen.findByLabelText("Max")).toBeInTheDocument()
    expect(screen.getByRole("heading", { name: "Capacity" })).toBeInTheDocument()
  })

  it("says so when the section does not exist", async () => {
    mockApi(routes())
    renderApp("/repositories/darkmem/nope")
    expect(await screen.findByText("This repository has no such section.")).toBeInTheDocument()
  })

  it("guards an unsaved change when another section is opened from the list", async () => {
    mockApi(routes())
    const { user } = renderApp("/repositories/darkmem/settings")
    const max = await screen.findByLabelText("Max")
    await user.clear(max)
    await user.type(max, "3")
    await user.click(within(screen.getByRole("navigation", { name: "Repository" })).getByRole("link", { name: "Overview" }))
    expect(await screen.findByText("Unsaved changes")).toBeInTheDocument()
  })

  it("shows the repository's state as a pill before the header actions", async () => {
    mockApi(routes())
    renderApp("/repositories/darkmem")
    const history = await screen.findByRole("link", { name: "View history" })
    const actions = history.closest("div") as HTMLElement
    expect(actions.firstElementChild?.querySelector("span[class*='rounded-full']") ?? actions.querySelector("span[class*='rounded-full']")).not.toBeNull()
  })
```

Run: `cd web && timeout 300 npx vitest run src/pages/repository.test.tsx`
Expected: FAIL: no navigation named "Repository".

- [ ] **Step 2: Read the section from the route, in `web/src/pages/repository.tsx`**

Replace `SECTIONS` and `RepositoryPage`, and give `RepositoryForm` a `section: string` prop:

```tsx
const SECTIONS = [
  { id: "", title: "Overview" },
  { id: "settings", title: "Capacity and labels" },
  { id: "labels", title: "Workflow labels" },
  { id: "registrations", title: "GitHub registrations" },
] as const

export function RepositoryPage() {
  const { name = "", section = "" } = useParams({ strict: false })
  // Keyed by name: each repository starts with its own empty draft, and
  // moving between its sections keeps the draft.
  return <RepositoryForm key={name} name={name} section={section} />
}
```

Delete the `SectionNav` import. Add:

```tsx
import { SideLayout } from "@/components/page/side-layout"
import { StatusPill } from "@/components/page/status-pill"
import { SIDE_LINK } from "@/lib/side-link"
import { repoStateWord } from "@/lib/status"
```

- [ ] **Step 3: Put the state pill first in the header actions**

In `HeaderActions`, make the pill the first child of the wrapper it returns:

```tsx
      {repo && (
        <span className="flex items-center">
          <StatusPill state={repoStateWord(repo).toLowerCase()} />
        </span>
      )}
```

The `PageHeader`'s `description` (the state sentence) stays.

- [ ] **Step 4: Lay the page out in sections**

In `RepositoryForm`'s final `return`, keep the `PageHeader`, the "Being removed…" line and the rejected alert. Replace everything from `<RepoActivity …/>` through the closing tag of `<div className="grid gap-6 xl:grid-cols-[minmax(0,1fr)_12rem]">` with:

```tsx
      <SideLayout
        label="Repository"
        nav={SECTIONS.map((s) =>
          s.id === "" ? (
            <Link key="overview" to="/repositories/$name" params={{ name }} activeOptions={{ exact: true }} className={SIDE_LINK}>
              {s.title}
            </Link>
          ) : (
            <Link key={s.id} to="/repositories/$name/$section" params={{ name, section: s.id }} className={SIDE_LINK}>
              {s.title}
            </Link>
          ),
        )}
      >
        {section === "" && <RepoActivity name={name} repo={repoStatus} now={now} />}
        {section === "settings" && <div className="flex min-w-0 flex-col gap-6">{FIELD_GROUPS}</div>}
        {section === "labels" && (
          <LabelCheckGroup name={name} effective={effective} degraded={degraded} degradedReason={status.data?.degraded_reason} disabled={locked} onAddLabel={addLabel} />
        )}
        {section === "registrations" && (
          <RegistrationsGroup name={name} degraded={degraded} degradedReason={status.data?.degraded_reason} disabled={locked} />
        )}
        {!SECTIONS.some((s) => s.id === section) && <p className="text-sm text-muted-foreground">This repository has no such section.</p>}
      </SideLayout>
```

`FIELD_GROUPS` stands for the three existing `<FieldGroup>` blocks (Capacity, Labels, Cleanup): move them, unchanged, into that `<div>`. `SaveBar` and `UnsavedGuard` stay after the layout, unchanged, so a draft is guarded on any navigation.

- [ ] **Step 5: Update the tests the layout reaches**

In `web/src/pages/repository.test.tsx`:

- delete the assertion on a navigation named "Sections" and its buttons;
- a test that fills or reads Max, Warm, labels or cleanup prefixes renders `/repositories/darkmem/settings`;
- a test of the label check renders `/repositories/darkmem/labels`;
- a test of the registrations renders `/repositories/darkmem/registrations`;
- a test of the chart, the facts list or the header stays on `/repositories/darkmem`.

In `web/src/a11y.test.tsx`, the repository case renders `/repositories/darkmem` and waits for the chart and for "ghr-aaaaaa". Keep the chart wait there, remove the "ghr-aaaaaa" wait, and add a second case directly after it that copies the first case's mocks, timeout and axe call, renders `/repositories/darkmem/registrations`, and waits for `await screen.findByText("ghr-aaaaaa")` before running axe.

- [ ] **Step 6: Run the checks**

Run: `cd web && timeout 300 npx vitest run src/pages/repository.test.tsx src/a11y.test.tsx src/router.test.tsx && timeout 300 npm run typecheck && timeout 300 npm run lint`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add web/src/pages/repository.tsx web/src/pages/repository.test.tsx web/src/a11y.test.tsx
git commit -m "feat(web): split the repository page into sections"
```

### Task 18: Settings area

**Files:**
- Modify: `web/src/pages/settings.tsx`
- Modify: `web/src/components/runner-section.tsx` (the group's title)
- Test: `web/src/pages/settings.test.tsx`, `web/src/components/runner-section.test.tsx`, `web/src/pages/toolchains.test.tsx`, `web/src/pages/storage.test.tsx`, `web/src/router.test.tsx`, `web/src/a11y.test.tsx`

**Interfaces:**
- Consumes: `/settings/$section` (Contracts, "Routes"); `SideLayout`, `SideGroup`, `SIDE_LINK` (Contracts, "Rows and side layout").
- Produces: nothing other tasks use.

**Items:** 8

**Depends on:** Task 8, Task 9, Task 17

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 2 = 4

- [ ] **Step 1: Add the failing tests to `web/src/pages/settings.test.tsx`**

The file has `routes(over?)` and renders with `mockApi(routes())` and `renderApp("/settings")`.

```tsx
  it("opens on General with the section list beside it", async () => {
    mockApi(routes())
    renderApp("/settings")
    const nav = await screen.findByRole("navigation", { name: "Settings" })
    expect(within(nav).getAllByRole("link").map((a) => a.textContent)).toEqual([
      "General",
      "GitHub token",
      "Runner software",
      "Account",
      "Toolchains",
      "Storage",
    ])
    expect(within(nav).getByText("Host")).toBeInTheDocument()
    expect(within(nav).getByRole("link", { name: "General" })).toHaveAttribute("aria-current", "page")
    expect(await screen.findByRole("heading", { level: 1, name: "General" })).toBeInTheDocument()
    expect(await screen.findByLabelText("Poll interval")).toBeInTheDocument()
    expect(screen.queryByRole("heading", { name: "GitHub token" })).not.toBeInTheDocument()
  })

  it.each([
    ["token", "GitHub token"],
    ["runner", "Runner software"],
    ["account", "Account"],
    ["toolchains", "Toolchains"],
    ["storage", "Storage"],
  ])("shows the %s section at its own address", async (section, title) => {
    mockApi(routes())
    renderApp(`/settings/${section}`)
    const nav = await screen.findByRole("navigation", { name: "Settings" })
    expect(within(nav).getByRole("link", { name: title })).toHaveAttribute("aria-current", "page")
    expect(within(nav).getByRole("link", { name: "General" })).not.toHaveAttribute("aria-current")
    expect(await screen.findByRole("heading", { level: 1, name: title })).toBeInTheDocument()
    expect(screen.queryByLabelText("Poll interval")).not.toBeInTheDocument()
  })

  it("says so when the section does not exist", async () => {
    mockApi(routes())
    renderApp("/settings/nope")
    expect(await screen.findByText("Settings has no such section.")).toBeInTheDocument()
  })

  it("guards an unsaved change when another section is opened from the list", async () => {
    mockApi(routes())
    const { user } = renderApp("/settings")
    const poll = await screen.findByLabelText("Poll interval")
    await user.clear(poll)
    await user.type(poll, "15s")
    await user.click(within(screen.getByRole("navigation", { name: "Settings" })).getByRole("link", { name: "GitHub token" }))
    expect(await screen.findByText("Unsaved changes")).toBeInTheDocument()
  })
```

If `routes()` lacks a route a section needs (`GET /api/token`, `GET /api/storage`), add it there from `fixtures`, as the toolchains and storage tests do.

Run: `cd web && timeout 300 npx vitest run src/pages/settings.test.tsx`
Expected: FAIL: no navigation named "Settings".

- [ ] **Step 2: Rewrite `web/src/pages/settings.tsx`**

```tsx
import { Link, useParams } from "@tanstack/react-router"
import { Spinner } from "darkraise-ui/components/spinner"
import { PageHeader } from "darkraise-ui/layout"
import type { ReactNode } from "react"
import { AccountSection } from "@/components/account-section"
import { ErrorLine } from "@/components/page/error-line"
import { SideGroup, SideLayout } from "@/components/page/side-layout"
import { RunnerSection } from "@/components/runner-section"
import { SaveBar } from "@/components/save-bar"
import { SettingsSections } from "@/components/settings-form"
import { TokenSection } from "@/components/token-section"
import { UnsavedGuard } from "@/components/unsaved-guard"
import { useSettingsForm } from "@/lib/settings-form"
import { SIDE_LINK } from "@/lib/side-link"
import { StoragePage } from "@/pages/storage"
import { ToolchainsPage } from "@/pages/toolchains"

const CONFIG = [
  { id: "", title: "General" },
  { id: "token", title: "GitHub token" },
  { id: "runner", title: "Runner software" },
  { id: "account", title: "Account" },
] as const

const HOST = [
  { id: "toolchains", title: "Toolchains" },
  { id: "storage", title: "Storage" },
] as const

function SectionLink({ id, title }: { id: string; title: string }) {
  return id === "" ? (
    <Link to="/settings" activeOptions={{ exact: true }} className={SIDE_LINK}>
      {title}
    </Link>
  ) : (
    <Link to="/settings/$section" params={{ section: id }} className={SIDE_LINK}>
      {title}
    </Link>
  )
}

// The save bar counts config fields only; the token, runner and account
// sections act at once with their own buttons.
function GeneralSettings() {
  const form = useSettingsForm()
  const c = form.config
  return (
    <>
      <PageHeader title="General" description={c ? `Serving ${c.owner} in ${c.mode} mode` : undefined} />
      {!c ? (
        form.configError ? <ErrorLine onRetry={form.retryConfig}>{form.configError}</ErrorLine> : <Spinner label="Loading" />
      ) : (
        <>
          <div className="flex min-w-0 flex-col gap-6">
            <SettingsSections form={form} />
          </div>
          <SaveBar count={form.changed.length} saving={form.saving} disabled={form.offline} onSave={() => void form.save()} onDiscard={form.discard} />
          <UnsavedGuard count={form.changed.length} page="Settings" saving={form.saving} onSave={form.save} onDiscard={form.discard} />
        </>
      )}
    </>
  )
}

function Titled({ title, children }: { title: string; children: ReactNode }) {
  return (
    <>
      <PageHeader title={title} />
      {children}
    </>
  )
}

function SectionContent({ section }: { section: string }) {
  switch (section) {
    case "":
      return <GeneralSettings />
    case "token":
      return (
        <Titled title="GitHub token">
          <TokenSection />
        </Titled>
      )
    case "runner":
      return (
        <Titled title="Runner software">
          <RunnerSection />
        </Titled>
      )
    case "account":
      return (
        <Titled title="Account">
          <AccountSection />
        </Titled>
      )
    case "toolchains":
      return <ToolchainsPage />
    case "storage":
      return <StoragePage />
    default:
      return <p className="text-sm text-muted-foreground">Settings has no such section.</p>
  }
}

export function SettingsPage() {
  const { section = "" } = useParams({ strict: false })
  return (
    <SideLayout
      label="Settings"
      nav={
        <>
          {CONFIG.map((s) => (
            <SectionLink key={s.id} id={s.id} title={s.title} />
          ))}
          <SideGroup>Host</SideGroup>
          {HOST.map((s) => (
            <SectionLink key={s.id} id={s.id} title={s.title} />
          ))}
        </>
      }
    >
      <SectionContent section={section} />
    </SideLayout>
  )
}
```

- [ ] **Step 3: Rename the runner group**

In `web/src/components/runner-section.tsx`, change the `FieldGroup`'s `title` from "Runner and config" to "Runner software". Nothing else in the file changes.

- [ ] **Step 4: Update the tests the layout reaches**

- `web/src/components/runner-section.test.tsx`: its `section()` helper finds the region "Runner and config"; change the name to "Runner software", and change each `renderApp("/settings")` in the file to `renderApp("/settings/runner")`.
- `web/src/pages/settings.test.tsx`: delete the assertions on a navigation named "Sections"; a test that uses the token section renders `/settings/token`, one that uses the account section renders `/settings/account`, one that uses the runner section renders `/settings/runner`; tests of the config form and the save bar stay on `/settings`.
- `web/src/router.test.tsx`: in "serves a settings section under /settings" expect the heading "GitHub token"; where another case expects the heading "Settings" after opening `/settings`, expect "General".
- `web/src/components/shell.test.tsx` is not touched: it looks for "Poll interval" on `/settings`, which General still shows.
- `web/src/pages/toolchains.test.tsx` and `web/src/pages/storage.test.tsx`: their existing renders of `/toolchains` and `/storage` stay. Add to each, with the mocks its first test uses:

```tsx
  it("is served inside Settings", async () => {
    renderApp("/settings/toolchains")
    expect(await screen.findByRole("heading", { level: 1, name: "Toolchains" })).toBeInTheDocument()
    expect(screen.getByRole("navigation", { name: "Settings" })).toBeInTheDocument()
  })
```

(with `/settings/storage` and "Storage" in the storage file), preceded by that file's usual `mockApi(…)` call.

- `web/src/a11y.test.tsx`: the settings case renders `/settings` and waits for the navigation "Sections" and the text "4,980 of 5,000, resets 14:45". Change the navigation's name to "Settings" and remove the token-text wait there; add a second case directly after it that copies the first case's mocks, timeout and axe call, renders `/settings/token`, and waits for `await screen.findByText("4,980 of 5,000, resets 14:45")`.

- [ ] **Step 5: Run the checks**

Run: `cd web && timeout 600 npm test && timeout 300 npm run typecheck && timeout 300 npm run lint`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add web/src/pages/settings.tsx web/src/pages/settings.test.tsx web/src/components/runner-section.tsx web/src/components/runner-section.test.tsx web/src/pages/toolchains.test.tsx web/src/pages/storage.test.tsx web/src/router.test.tsx web/src/a11y.test.tsx
git commit -m "feat(web): gather settings, toolchains and storage"
```

### Task 19: Redirects and links

**Files:**
- Modify: `web/src/router.tsx` (`toolchainsRoute`, `storageRoute`)
- Modify: `web/src/pages/dashboard.tsx` (the "Storage" link)
- Test: `web/src/router.test.tsx`, `web/src/pages/dashboard.test.tsx`, `web/src/a11y.test.tsx`

**Interfaces:**
- Consumes: Contracts, "Routes".
- Produces: nothing other tasks use.

**Items:** 3, 8

**Depends on:** Task 18

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 1 = 3

- [ ] **Step 1: Add the failing tests**

In `web/src/router.test.tsx` (add `waitFor` to the `@testing-library/react` import if it is missing):

```tsx
  it.each([
    ["/toolchains", "/settings/toolchains", ""],
    ["/storage", "/settings/storage", ""],
    ["/toolchains#installed", "/settings/toolchains", "installed"],
    ["/storage#prune", "/settings/storage", "prune"],
  ])("sends %s to its settings section, keeping the hash", async (from, to, hash) => {
    mockApi(authedRoutes())
    const { router } = renderApp(from)
    await waitFor(() => expect(router.state.location.pathname).toBe(to))
    expect(router.state.location.hash).toBe(hash)
  })
```

In `web/src/pages/dashboard.test.tsx`:

```tsx
  it("links the disk box to the storage section", async () => {
    mockApi(authedRoutes())
    renderApp("/")
    expect(await region("Disk").findByRole("link", { name: "Storage" })).toHaveAttribute("href", "/settings/storage")
  })
```

`region(name)` is the file's helper; it queries synchronously, so if the Disk region is not yet rendered, first `await screen.findByRole("region", { name: "Disk" })`.

Run: `cd web && timeout 300 npx vitest run src/router.test.tsx src/pages/dashboard.test.tsx`
Expected: FAIL on the five new cases.

- [ ] **Step 2: Turn the two routes into redirects, in `web/src/router.tsx`**

```ts
const toolchainsRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/toolchains",
  beforeLoad: ({ location }) => {
    throw redirect({ to: "/settings/$section", params: { section: "toolchains" }, hash: location.hash, replace: true })
  },
})
const storageRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/storage",
  beforeLoad: ({ location }) => {
    throw redirect({ to: "/settings/$section", params: { section: "storage" }, hash: location.hash, replace: true })
  },
})
```

Remove the `ToolchainsPage` and `StoragePage` imports from `router.tsx`.

- [ ] **Step 3: Point the Dashboard's link at the section**

In `web/src/pages/dashboard.tsx`, the Disk section's link becomes:

```tsx
                <Link to="/settings/$section" params={{ section: "storage" }} className="ml-auto text-sm text-primary hover:underline">
                  Storage
                </Link>
```

Then list any other in-app reference: `cd web && rg -n 'to="/toolchains"|to="/storage"|to: "/toolchains"|to: "/storage"|href="/toolchains"|href="/storage"' src --glob '!*.test.*'`. Each hit outside `router.tsx` takes the same `to="/settings/$section"` form with its section. Then check the wording: `rg -n "Toolchains page|Storage page|on the Toolchains|on the Storage" src --glob '!*.test.*'`; a toast or help line that sends the reader to the "Toolchains page" or "Storage page" says "Toolchains in Settings" or "Storage in Settings" instead, and its test follows.

- [ ] **Step 4: Open the two pages by their new addresses in the tests**

In `web/src/a11y.test.tsx`, change `renderApp("/toolchains")` to `renderApp("/settings/toolchains")` and `renderApp("/storage")` to `renderApp("/settings/storage")`. In `web/src/pages/toolchains.test.tsx` and `web/src/pages/storage.test.tsx`, leave the renders of `/toolchains` and `/storage`: they now pass through the redirect; change only an assertion that reads the pathname, to the new one.

- [ ] **Step 5: Run the checks**

Run: `cd web && timeout 600 npm test && timeout 300 npm run typecheck && timeout 300 npm run lint`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add web/src/router.tsx web/src/router.test.tsx web/src/pages/dashboard.tsx web/src/pages/dashboard.test.tsx web/src/a11y.test.tsx
git add -u web/src
git commit -m "feat(web): redirect the old toolchains and storage URLs"
```

### Task 20: Cleanup and release checks

**Files:**
- Delete: `web/src/components/page/state-text.tsx`, `web/src/components/page/state-text.test.tsx`, `web/src/components/page/section-nav.tsx`, `web/src/components/page/section-nav.test.tsx`
- Modify: every file under `web/src` that imports `StateText` (import and tag rename only), and every non-test file under `web/src` that still uses `rounded-[10px]` or `font-medium`
- Modify: `docs/superpowers/registers/2026-10-10-ghr-forge-ui.md` (through `scripts/register`)

**Interfaces:**
- Consumes: everything above.
- Produces: nothing.

**Items:** 1, 2, 3, 4, 5, 6, 7, 8

**Depends on:** Task 19

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 1 = 3

- [ ] **Step 1: Rename `StateText` to `StatusPill` everywhere**

```bash
cd web && for f in $(rg -l "StateText" src); do
  sed -i 's#@/components/page/state-text#@/components/page/status-pill#g; s#\./state-text#./status-pill#g; s#\bStateText\b#StatusPill#g' "$f"
done
git rm -q -f src/components/page/state-text.tsx src/components/page/state-text.test.tsx
rg -n "StateText|state-text" src
```

Expected: the last `rg` prints nothing. If a file now imports `StatusPill` twice, merge the two imports. `state-text.test.tsx`'s word assertions are covered by `status-pill.test.tsx`.

- [ ] **Step 2: Delete the section index**

```bash
cd web && rg -n "SectionNav|section-nav" src
```

Expected: hits only in `src/components/page/section-nav.tsx` and its test. Then `git rm -q src/components/page/section-nav.tsx src/components/page/section-nav.test.tsx`. If another file still imports it, stop and report the file.

- [ ] **Step 3: Finish the shape and weight rules in ghr's own markup**

```bash
cd web && rg -l "rounded-\[10px\]" src --glob '!*.test.*' | xargs -r sed -i 's/rounded-\[10px\]/rounded-md/g'
rg -l "\bfont-medium\b" src --glob '!*.test.*' | xargs -r sed -i 's/\bfont-medium\b/font-semibold/g'
rg -n "rounded-\[10px\]|\bfont-medium\b|IBM Plex" src public
```

Expected: the last `rg` prints hits in test files only, or nothing. Do not edit the `--surface-sidebar` or `--sidebar-*` lines in `ghr-theme.css`: the theme engine writes those keys and `styles/theme.test.ts` requires an override for each.

- [ ] **Step 4: Run every check against the final tree**

```bash
cd web && timeout 600 npm test && timeout 300 npm run typecheck && timeout 300 npm run lint && timeout 600 npm run build
cd .. && export PATH=$PATH:/usr/local/go/bin && test -z "$(gofmt -l internal cmd web/*.go)" && timeout 300 go vet ./internal/... ./cmd/... ./web/ && timeout 900 go test ./internal/... ./cmd/... ./web/
```

Expected: every command passes. A test that asserts `rounded-[10px]` or `font-medium` on an element Step 3 changed now expects `rounded-md` or `font-semibold`; update it and run the whole block again. Make no source change after this step.

- [ ] **Step 5: Record the register rows**

For each row 1 to 8 of `docs/superpowers/registers/2026-10-10-ghr-forge-ui.md`:

```bash
bash /root/.claude/plugins/cache/darkraise/dr-superpowers/1.33.0/scripts/register set docs/superpowers/registers/2026-10-10-ghr-forge-ui.md <id> verify --note "Automated checks pass; the manual checks of spec section 7 are the owner's, on the LXC."
```

- [ ] **Step 6: Commit**

```bash
git add -A web/src docs/superpowers/registers/2026-10-10-ghr-forge-ui.md
git commit -m "refactor(web): drop the state text alias and section index"
```

- [ ] **Step 7: Report the manual checks**

Do not deploy. Report to the owner that these checks from spec §7 remain, each needing their yes for the deploy to the runner LXC:

- every page in light and in dark, at 1440px and at 400px wide, with no sideways page scroll;
- a real run in progress on the Dashboard, Actions, its run page and its job page;
- a failed run's job page: the first failed step open, the error line marked, the search box dark in light mode;
- General and "Capacity and labels": the unsaved guard on a left-list click;
- `/toolchains` and `/storage` land on the new sections;
- a new browser profile opens in the system's mode;
- the tab row: the coral line under the current tab, the divider before Repositories, sideways scroll on a phone.
