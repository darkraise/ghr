# ghr Forge UI Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: the skill the **Execution:** line names — dr-superpowers:subagent-driven-development for `subagent`, dr-superpowers:executing-plans for `inline`. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** ghr's web UI takes the Forge look (GitHub's palette, Mona Sans, boxes, status pills), a top bar with tabs in place of the sidebar, and GitHub-style run lists, run pages, repository rows and one settings area.

**Architecture:** Frontend only, in `web/`. ghr keeps overriding darkraise-ui's tokens from `web/src/styles/ghr-theme.css`; a foundation (fonts, palette, mode, pill, box, shell, shared row and side layout, section routes) lands first, then each page is rebuilt on those pieces. Two new routes carry a `$section` parameter so the route table stays fixed while the pages change.

**Tech Stack:** React 19, TypeScript 5.7, Tailwind 4, darkraise-ui 6.9, TanStack Router and Query, lucide-react, Vitest with Testing Library under jsdom.

**Spec:** docs/superpowers/specs/2026-10-10-ghr-forge-ui-design.md

**Execution:** subagent — `claude --model sonnet --effort high` — the plan runs parallel waves.

**Parallelism:** waves — 1 | 2-3 | 4-6 | 7-9 | 10-12 | 13-15 | 16-18 | 19 | 20

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
- Shape: `rounded-md` (6px) on boxes, buttons and inputs; `rounded-full` only on status pills and count badges. 1px `border-border`. No shadows, no gradients, no backdrop blur (`web/src/anti-slop.test.ts` fails on them). Do not import the kit's `Badge`.
- Icons are lucide-react at `size={15}`, with `aria-hidden="true"` unless the icon carries the meaning, in which case `role="img"` and an `aria-label`.
- Mono (`font-mono`) for names, IDs, run numbers, branches, durations and times. Weights 400 and 600 only (`font-semibold`), no `font-medium` in new code.
- Wording, validation, dialogs, toasts, polling and URL parameters do not change unless a task says so.
- Not built: search, re-running jobs, log timestamp or wrap toggles.
- English only. Comments only where the why is not obvious. Commits: `<type>(<scope>): <subject>`, subject at most 50 characters, imperative, no period.

## Contracts

**Tokens and classes (Task 2)**

- CSS custom properties on both mode blocks of `web/src/styles/ghr-theme.css`: `--running`, `--top-bar`, `--tab-line`, as HSL triplets.
- Tailwind colour `running`: `text-running`, `bg-running`, `bg-running/15`, `fill-running`, `stroke-running`, `border-running`.
- `web/src/lib/palette.ts` exports `PALETTE: Record<Role, { light: string; dark: string }>` with roles `ground`, `surface`, `topBar`, `sunk`, `line`, `text`, `muted`, `accent`, `onAccent`, `tabLine`, `succeeded`, `failed`, `running`, `waiting`; `PILL_ALPHA = 0.15`; `contrast(a: string, b: string): number`; `over(fg: string, bg: string, alpha: number): string`; `hslTriplet(hex: string): string`.

**Status (Task 4)**

- `web/src/lib/status.ts`: `type Tone = "accent" | "run" | "warn" | "bad" | "ok" | "muted"`; `stateTone(state: string): Tone` maps `busy`, `running`, `active`, `in_progress` to `run`.
- `web/src/components/page/status-pill.tsx`: `StatusPill({ state, label }: { state: string; label?: string })`. The word is `label`, or `state` with its first letter capitalised.
- `web/src/components/page/state-text.tsx` re-exports `StatusPill` as `StateText` until Task 20 deletes it.

**Box (Task 5)**

- `Section({ id?, title, aside?, children, className?, flush? })` in `web/src/components/page/section.tsx`. `flush` removes the body padding, for bodies that are a list or table.

**Rows and side layout (Task 8)**

- `web/src/components/page/run-row.tsx`:
  - `RunRow({ icon, title, facts?, values?, actions?, wide }: { icon: ReactNode; title: ReactNode; facts?: ReactNode; values?: ReactNode; actions?: ReactNode; wide: boolean })` renders one `<tr>`: an icon cell, a main cell (title, facts, and `values` under them when `wide` is false), a right-aligned values cell when `wide`, and an actions cell when `actions` is given.
  - `RunValue({ kind, children }: { kind: "time" | "duration"; children: ReactNode })`.
  - `RunGroup({ label, cols }: { label: string; cols: number })` renders the group heading `<tr>` holding a `<th scope="rowgroup">`.
  - `runCols(wide: boolean, actions: boolean): number`.
- `web/src/components/page/side-layout.tsx`: `SideLayout({ label, nav, children })`, `SideGroup({ children })`, and the class string `SIDE_LINK` for links that carry `aria-current="page"` when current.

**Routes (Task 9)**

- `/settings/$section` (route id `/app/settings/$section`) and `/repositories/$name/$section` (route id `/app/repositories/$name/$section`). Pages read parameters with `useParams({ strict: false })`.
- Settings sections: `token`, `runner`, `account`, `toolchains`, `storage`; no section means General. Repository sections: `settings`, `labels`, `registrations`; no section means Overview.

**Update banner (Task 6)**

- `UpdateCard({ update, offline })` keeps its name, props, states and wording; it lays out as one horizontal banner.

## Assumptions (evidence)

- `TopNavLayout` takes the same `LayoutProps` as `SidebarLayout` and scrolls its bar sideways when items outgrow it: `web/node_modules/darkraise-ui/dist/layout/top-nav-layout/TopNavLayout.d.ts`, read 2026-10-10. Its nav items carry the class `dr-top-nav-layout-nav-item` (grep of the kit's `dist`, 2026-10-10). How it marks the current item and separates groups is unverified — Task 7 verifies it.
- `@fontsource-variable/mona-sans` 5.3.0 and `@fontsource/jetbrains-mono` 5.3.0 are OFL-1.1: `npm view`, 2026-10-10. The font family names and CSS entry files are unverified — Task 1 verifies them.
- Palette contrast: every text and status colour is at least 4.57:1 on `surface` and `ground` in its mode (computed 2026-10-10 from the hex values; Task 2's test asserts it).
- A pill's coloured word cannot reach 4.5:1 on its own tint (light Running: about 4.3:1), so the word uses the text colour; spec §1.4 was amended for this on 2026-10-10.
- `theme.config.ts` accepts `mode: "system"`: `ModeControl` already calls `setMode("system")` (`web/src/components/mode-control.tsx`). Unverified for `defaults` — Task 3 verifies it with the typecheck.
- The kit's `PageHeader` `title` prop type is unverified — Task 14 verifies it.
- `web/src/anti-slop.test.ts` forbids gradients, backdrop blur, `bg-clip-text` and the kit `Badge`; it has no rule on pills or inset markers (read 2026-10-10).
- The run page's summary is already a four-fact `dl` and Actions already renders title-plus-facts rows (`web/src/pages/run.tsx:154-190`, `web/src/pages/actions.tsx:32-75`, read 2026-10-10), so Tasks 12 and 14 restyle more than they restructure.

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
Expected: PASS. If another test asserts an old colour value, update that assertion to the new token's value.

- [ ] **Step 6: Commit**

```bash
git add web/src/lib/palette.ts web/src/lib/palette.test.ts web/src/styles/ghr-theme.css
git commit -m "feat(web): set the Forge palette"
```

### Task 3: System colour mode by default

**Files:**
- Modify: `web/public/theme-init.js` (the `var dark =` statement)
- Modify: `web/src/theme.config.ts` (`defaults.mode`)
- Test: `web/src/theme-init.test.ts`, `web/src/components/mode-control.test.tsx`

**Interfaces:**
- Consumes: nothing.
- Produces: nothing other tasks use.

**Items:** 4

**Depends on:** none

**Implementer:** dr-superpowers:impl-sonnet-low
**Evaluation:** files 1 - spec 0 - coupling 0 - risk 1 = 2

- [ ] **Step 1: Add the failing tests to `web/src/theme-init.test.ts`**

Add inside the existing `describe`, and import `setViewport`-free stubbing as shown (the script reads only `matches`):

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

  it("keeps a saved dark mode on a light system", () => {
    const restore = prefersDark(false)
    try {
      localStorage.setItem("mode", "dark")
      run()
      expect(root.getAttribute("data-mode")).toBe("dark")
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

- [ ] **Step 5: Update `web/src/components/mode-control.test.tsx`**

Rename the test "starts on dark and saves the chosen mode" to "starts on system and saves the chosen mode". If it asserts the initially checked radio, expect the one named "System". Leave the rest of the test as it is.

- [ ] **Step 6: Run the checks**

Run: `cd web && timeout 300 npx vitest run src/theme-init.test.ts src/components/mode-control.test.tsx && timeout 300 npm run typecheck`
Expected: PASS, and the typecheck accepts `mode: "system"`. If the typecheck rejects it, stop and report: the kit's `ThemeConfig` then needs another way to default to the system.

- [ ] **Step 7: Commit**

```bash
git add web/public/theme-init.js web/src/theme.config.ts web/src/theme-init.test.ts web/src/components/mode-control.test.tsx
git commit -m "feat(web): follow the system colour mode by default"
```

### Task 4: Status pill and tones

**Files:**
- Modify: `web/src/lib/status.ts` (`Tone`, `tones`)
- Create: `web/src/components/page/status-pill.tsx`
- Test: `web/src/components/page/status-pill.test.tsx`
- Modify: `web/src/components/page/state-text.tsx`, `web/src/components/page/state-text.test.tsx`

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
import { stateTone } from "@/lib/status"
import { StatusPill } from "./status-pill"

describe("StatusPill", () => {
  it("shows the state as a capitalised word", () => {
    render(<StatusPill state="running" />)
    expect(screen.getByText("Running")).toBeInTheDocument()
  })

  it("shows a given label in place of the state", () => {
    render(<StatusPill state="success" label="Up to date" />)
    expect(screen.getByText("Up to date")).toBeInTheDocument()
    expect(screen.queryByText("Success")).not.toBeInTheDocument()
  })

  it.each([
    ["running", "run", "text-running"],
    ["busy", "run", "text-running"],
    ["in_progress", "run", "text-running"],
    ["queued", "warn", "text-warning"],
    ["failed", "bad", "text-destructive"],
    ["success", "ok", "text-success"],
    ["online", "accent", "text-primary"],
    ["paused", "muted", "text-muted-foreground"],
  ])("colours the icon of %s by its tone", (state, tone, iconClass) => {
    expect(stateTone(state)).toBe(tone)
    const { container } = render(<StatusPill state={state} />)
    expect(container.querySelector("svg")).toHaveClass(iconClass)
    expect(container.querySelector("svg")).toHaveAttribute("aria-hidden", "true")
  })

  it("keeps the word in the text colour", () => {
    render(<StatusPill state="failed" />)
    expect(screen.getByText("Failed")).toHaveClass("text-foreground")
  })
})
```

- [ ] **Step 2: Run it to see it fail**

Run: `cd web && timeout 300 npx vitest run src/components/page/status-pill.test.tsx`
Expected: FAIL, cannot resolve `./status-pill`.

- [ ] **Step 3: Change the tones in `web/src/lib/status.ts`**

Replace the `Tone` type and the first three entries of `tones`, and add `in_progress`:

```ts
export type Tone = "accent" | "run" | "warn" | "bad" | "ok" | "muted"

const tones: Record<string, Tone> = {
  busy: "run",
  running: "run",
  active: "run",
  in_progress: "run",
  online: "accent",
  matched: "accent",
```

The rest of the map stays as it is.

- [ ] **Step 4: Write `web/src/components/page/status-pill.tsx`**

```tsx
import { Check, CircleDot, Clock, Dot, Minus, X, type LucideIcon } from "lucide-react"
import { stateTone, type Tone } from "@/lib/status"

const TONES: Record<Tone, { Icon: LucideIcon; icon: string; fill: string }> = {
  ok: { Icon: Check, icon: "text-success", fill: "bg-success/15" },
  bad: { Icon: X, icon: "text-destructive", fill: "bg-destructive/15" },
  run: { Icon: CircleDot, icon: "text-running", fill: "bg-running/15" },
  warn: { Icon: Clock, icon: "text-warning", fill: "bg-warning/15" },
  accent: { Icon: Dot, icon: "text-primary", fill: "bg-primary/15" },
  muted: { Icon: Minus, icon: "text-muted-foreground", fill: "bg-muted-foreground/15" },
}

const sentence = (s: string) => (s.charAt(0).toUpperCase() + s.slice(1)).replace(/_/g, " ")

// The word stays in the text colour: a coloured word on its own tint falls
// under 4.5:1 for light-mode Running.
export function StatusPill({ state, label }: { state: string; label?: string }) {
  const { Icon, icon, fill } = TONES[stateTone(state)]
  return (
    <span className={`inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-xs font-semibold whitespace-nowrap ${fill}`}>
      <Icon size={13} aria-hidden="true" className={`shrink-0 ${icon}`} />
      <span className="text-foreground">{label ?? sentence(state)}</span>
    </span>
  )
}
```

- [ ] **Step 5: Make `StateText` the pill**

Replace the whole of `web/src/components/page/state-text.tsx` with:

```tsx
// Kept so call sites compile until they are renamed to StatusPill.
export { StatusPill as StateText } from "./status-pill"
```

In `web/src/components/page/state-text.test.tsx`, keep every assertion about the word shown, and delete assertions about colour classes on the word (the pill test covers colour).

- [ ] **Step 6: Run the tests and fix what the tone change reaches**

Run: `cd web && timeout 600 npm test && timeout 300 npm run typecheck`
Expected: PASS. Where the typecheck reports a `Record<Tone, …>` missing `run`, add `run` with the `running` colour class of that map's kind (`text-running`, `bg-running`, `fill-running` or `stroke-running`). Where a test expects `text-primary` for a running, busy or active state, expect `text-running`.

- [ ] **Step 7: Commit**

```bash
git add web/src/lib/status.ts web/src/components/page/status-pill.tsx web/src/components/page/status-pill.test.tsx web/src/components/page/state-text.tsx web/src/components/page/state-text.test.tsx
git add -u web/src
git commit -m "feat(web): show states as pills"
```

### Task 5: Box section

**Files:**
- Modify: `web/src/components/page/section.tsx`
- Test: `web/src/components/page/section.test.tsx`

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

- [ ] **Step 4: Run the tests**

Run: `cd web && timeout 600 npm test`
Expected: PASS. If an older assertion in `section.test.tsx` names a removed class (`rounded-[10px]`, `p-4` on the section, `text-base`), update it to the class above that replaced it.

- [ ] **Step 5: Commit**

```bash
git add web/src/components/page/section.tsx web/src/components/page/section.test.tsx
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

Use the file's existing render helper and a fixture update with a deadline (the "due" state its other tests use):

```tsx
  it("lays out as one horizontal banner", () => {
    renderCard(dueUpdate)
    const banner = screen.getByRole("region", { name: "Runner update" })
    expect(banner).toHaveClass("flex-wrap", "items-center", "rounded-md")
    expect(banner).not.toHaveClass("flex-col")
  })
```

If the file has no `renderCard` or `dueUpdate`, name them after the helper and fixture its "due" test already uses.

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
- Modify: `web/src/styles/ghr-theme.css` (shell rules: remove the sidebar rules, add the top-bar rules)
- Test: `web/src/components/shell.test.tsx`, `web/src/components/capacity-bar.test.tsx`

**Interfaces:**
- Consumes: `--top-bar`, `--tab-line` and the `running` colour (Contracts, "Tokens and classes"); `UpdateCard` (Contracts, "Update banner").
- Produces: the app shell; nothing other tasks import.

**Items:** 3

**Depends on:** Task 2, Task 6

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 1 - coupling 1 - risk 1 = 4

- [ ] **Step 1: Learn how the kit marks tabs**

```bash
cd web && f=$(rg -l "dr-top-nav-layout-nav-item" node_modules/darkraise-ui/dist -g '*.js' | head -1) && rg -n -o ".{200}dr-top-nav-layout-nav-item.{300}" "$f" | head -5
rg -n "dr-top-nav-layout|dr-layout-header" node_modules/darkraise-ui/dist/styles.css | head -40
```

Note three facts for Step 4: the attribute or class on the current item (the sidebar uses `.active` and `[data-status="active"]`), the element that wraps the items, and whether group labels or a separator are rendered between the two nav groups.

- [ ] **Step 2: Rewrite the shell tests that name the sidebar**

In `web/src/components/shell.test.tsx`:

- "lists the pages in two groups": expect the links Dashboard, Runners, Actions, History, Repositories, Settings, in that order, inside the navigation landmark, and expect no link named Toolchains or Storage.
- "logs out from the rail": rename to "logs out from the top bar"; it clicks the button named "Log out".
- "shows runner capacity and the update card on every page" and "puts the capacity readout and the user menu in the header bar": expect one `role="img"` capacity element whose name is the fixture's capacity label, the capacity text, and the region "Runner update" when the fixture status has an update deadline.
- Add:

```tsx
  it("keeps Toolchains and Storage out of the tabs", async () => {
    renderApp("/")
    await screen.findByRole("link", { name: /Dashboard/ })
    expect(screen.queryByRole("link", { name: "Toolchains" })).not.toBeInTheDocument()
    expect(screen.queryByRole("link", { name: "Storage" })).not.toBeInTheDocument()
    expect(screen.getByRole("link", { name: /Settings/ })).toHaveAttribute("href", "/settings")
  })
```

Run: `cd web && timeout 300 npx vitest run src/components/shell.test.tsx`
Expected: FAIL on the new test (a Toolchains link exists).

- [ ] **Step 3: Rewrite the layout in `web/src/components/shell.tsx`**

Change the import `SidebarLayout` to `TopNavLayout` (same module), drop the `Boxes`-style unused icons only if lint reports them, and replace `navGroups` and the returned layout:

```tsx
function navGroups(status: Status | undefined, config: Config | undefined): NavGroup[] {
  return [
    {
      label: "Operate",
      items: [
        { label: "Dashboard", href: "/", icon: LayoutDashboard },
        { label: "Runners", href: "/runners", icon: Boxes, badge: count(status && running(status)) },
        { label: "Actions", href: "/actions", icon: Workflow },
        { label: "History", href: "/history", icon: HistoryIcon },
      ],
    },
    {
      label: "Configure",
      items: [
        { label: "Repositories", href: "/repositories", icon: GitBranch, badge: count(config?.repos?.length) },
        { label: "Settings", href: "/settings", icon: Settings },
      ],
    },
  ]
}
```

```tsx
    <TopNavLayout
      nav={navGroups(st, config.data)}
      showThemeSwitcher={false}
      notificationSlot={null}
      headerSlot={
        <div className="flex items-center gap-3">
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
            <span>Log out</span>
          </Button>
        </div>
      }
      user={{ name: "Owner", email: "ghr" }}
      onLogout={() => void logout()}
    >
      <div className="mx-auto flex w-full max-w-[1216px] flex-col">
        {/* the three existing Alert banners stay here, unchanged, in their current order */}
        {st && (
          <div className="mb-4 empty:hidden">
            <UpdateCard update={st.runner_update} offline={offline} />
          </div>
        )}
        <Outlet />
      </div>
    </TopNavLayout>
```

Move the three existing `Alert` blocks (Reconnecting, Degraded, First-run setup) to where the comment stands, unchanged. Import `CapacitySegments` in place of `CapacityBar`. Keep every hook, effect and the `logout` function as they are; the `--ghr-host` effect stays.

- [ ] **Step 4: Retire the rail in `capacity-bar.tsx` and style the top bar**

In `web/src/components/capacity-bar.tsx`: delete `CapacityBar` and the `tone` prop (free segments are always `bg-muted`), and change the busy colours:

```tsx
const KIND_CLASS: Record<Exclude<SegmentKind, "free">, string> = {
  busy: "ghr-pulse bg-running",
  starting: "bg-running/45",
  warm: "border border-running",
}
```

In `web/src/components/capacity-bar.test.tsx`, delete the `CapacityBar` and rail-tone cases and expect `bg-running` where a case expects `bg-primary`.

In `web/src/styles/ghr-theme.css`, delete the rules for `.dr-sidebar-*`, `.dr-sidebar-layout-*`, `.dr-mobile-drawer-content`, `--sidebar-width-expanded`, the light-mode rail block that rebinds tokens under `.dr-sidebar-layout-aside`, and the `@media (min-width: 768px)` rule that hides `.dr-layout-header`. Then add, using the current-item selector found in Step 1 in place of `[data-status="active"]` if it differs:

```css
.dr-layout-header {
  background: hsl(var(--top-bar));
  border-color: hsl(var(--border));
}

.dr-top-nav-layout-nav {
  background: hsl(var(--top-bar));
  border-bottom: 1px solid hsl(var(--border));
  overflow-x: auto;
}

.dr-top-nav-layout-nav-item {
  border-radius: 6px 6px 0 0;
  color: hsl(var(--foreground));
  font-weight: 400;
  white-space: nowrap;
}

.dr-top-nav-layout-nav-item.active,
.dr-top-nav-layout-nav-item[data-status="active"] {
  background: none;
  box-shadow: inset 0 -2px 0 hsl(var(--tab-line));
  font-weight: 600;
}
```

For the divider between the two groups: if Step 1 showed the kit renders a separator or group wrapper, give it `border-left: 1px solid hsl(var(--border))` with `margin: 0 0.5rem`; if it renders the six items as siblings, add that border and margin to the fifth item with `.dr-top-nav-layout-nav-item:nth-of-type(5)`. If the kit prints the group labels "Operate" and "Configure", hide them visually with the kit's own label class and `display: none`.

Keep `.dr-brand-logo-label` and its `::after` host rule, replacing `--sidebar-foreground-muted` with `--muted-foreground`; below 640px hide the host: `@media (max-width: 639px) { .dr-brand-logo-label::after { content: none; } }`.

- [ ] **Step 5: Run the checks**

Run: `cd web && timeout 600 npm test && timeout 300 npm run typecheck && timeout 300 npm run lint`
Expected: PASS. A page test that looked for the capacity panel "Runners" in the sidebar now finds the capacity in the header; update such an assertion to the `role="img"` element.

- [ ] **Step 6: Look at it**

Run: `cd web && timeout 600 npm run build`
Expected: the build passes. If the kit could not give the current-tab line, the divider, or a tab row that scrolls sideways, stop and report which of the three is missing, with the Step 1 output.

- [ ] **Step 7: Commit**

```bash
git add web/src/components/shell.tsx web/src/components/shell.test.tsx web/src/components/capacity-bar.tsx web/src/components/capacity-bar.test.tsx web/src/styles/ghr-theme.css
git add -u web/src
git commit -m "feat(web): replace the sidebar with a top bar and tabs"
```

### Task 8: Run row and side layout

**Files:**
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
import { runCols, RunGroup, RunRow, RunValue } from "./run-row"

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

describe("RunRow", () => {
  it("puts the values in their own right-hand cell when wide", () => {
    table(true)
    const cells = within(screen.getAllByRole("row")[1] as HTMLElement).getAllByRole("cell")
    expect(cells).toHaveLength(3)
    expect(cells[1]).toHaveTextContent("buildCI #7")
    expect(cells[2]).toHaveTextContent("14:023m10s")
  })

  it("moves the values under the facts when narrow", () => {
    table(false)
    const cells = within(screen.getAllByRole("row")[1] as HTMLElement).getAllByRole("cell")
    expect(cells).toHaveLength(2)
    expect(cells[1]).toHaveTextContent("buildCI #714:023m10s")
  })

  it("adds an actions cell only when actions are given", () => {
    table(true, true)
    expect(within(screen.getAllByRole("row")[1] as HTMLElement).getAllByRole("cell")).toHaveLength(4)
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
import { SIDE_LINK, SideGroup, SideLayout } from "./side-layout"

describe("SideLayout", () => {
  it("names the navigation and shows the content beside it", () => {
    render(
      <SideLayout
        label="Settings"
        nav={
          <>
            <a href="/settings" aria-current="page" className={SIDE_LINK}>
              General
            </a>
            <SideGroup>Host</SideGroup>
            <a href="/settings/storage" className={SIDE_LINK}>
              Storage
            </a>
          </>
        }
      >
        <p>content</p>
      </SideLayout>,
    )
    const nav = screen.getByRole("navigation", { name: "Settings" })
    expect(within(nav).getByRole("link", { name: "General" })).toHaveAttribute("aria-current", "page")
    expect(within(nav).getByText("Host")).toBeInTheDocument()
    expect(nav).not.toContainElement(screen.getByText("content"))
  })

  it("marks the current link by its aria-current attribute", () => {
    expect(SIDE_LINK).toContain("aria-[current=page]:bg-muted")
    expect(SIDE_LINK).toContain("aria-[current=page]:shadow-[inset_2px_0_0_hsl(var(--primary))]")
  })
})
```

- [ ] **Step 2: Run them to see them fail**

Run: `cd web && timeout 300 npx vitest run src/components/page/run-row.test.tsx src/components/page/side-layout.test.tsx`
Expected: FAIL, cannot resolve `./run-row` and `./side-layout`.

- [ ] **Step 3: Write `web/src/components/page/run-row.tsx`**

```tsx
import { Calendar, Clock } from "lucide-react"
import type { ReactNode } from "react"

export function runCols(wide: boolean, actions: boolean): number {
  return 2 + (wide ? 1 : 0) + (actions ? 1 : 0)
}

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
        <td className="w-40 py-2.5 pr-4 align-top">
          <span className="flex flex-col gap-1">{values}</span>
        </td>
      )}
      {actions && <td className="w-20 py-2.5 pr-3 text-right align-top">{actions}</td>}
    </tr>
  )
}
```

- [ ] **Step 4: Write `web/src/components/page/side-layout.tsx`**

```tsx
import type { ReactNode } from "react"

// The router sets aria-current="page" on the current link, so the style follows it.
export const SIDE_LINK =
  "flex items-center gap-2 rounded-md px-3 py-1.5 text-sm hover:bg-muted aria-[current=page]:bg-muted aria-[current=page]:font-semibold aria-[current=page]:shadow-[inset_2px_0_0_hsl(var(--primary))]"

export function SideGroup({ children }: { children: ReactNode }) {
  return <p className="mt-3 border-t border-border px-3 pt-3 pb-1 text-xs font-semibold text-muted-foreground">{children}</p>
}

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

- [ ] **Step 5: Run the tests**

Run: `cd web && timeout 300 npx vitest run src/components/page/run-row.test.tsx src/components/page/side-layout.test.tsx && timeout 300 npm run lint`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add web/src/components/page/run-row.tsx web/src/components/page/run-row.test.tsx web/src/components/page/side-layout.tsx web/src/components/page/side-layout.test.tsx
git commit -m "feat(web): add the run row and the side layout"
```

### Task 9: Section routes

**Files:**
- Modify: `web/src/router.tsx` (after `settingsRoute`, and the `appRoute.addChildren` list)
- Test: `web/src/router.test.tsx`

**Interfaces:**
- Consumes: nothing.
- Produces: Contracts, "Routes".

**Items:** 7, 8

**Depends on:** none

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 1 = 4

- [ ] **Step 1: Add the failing test to `web/src/router.test.tsx`**

Inside `describe("routes", …)`, using the file's existing mock helper for an authenticated visitor (the one its "reads the runner detail tab from the search" test uses):

```tsx
  it("serves a settings section under /settings", async () => {
    mockApi(authedRoutes())
    const { router } = renderApp("/settings/token")
    expect(await screen.findByRole("heading", { level: 1, name: "Settings" })).toBeInTheDocument()
    expect(router.state.location.pathname).toBe("/settings/token")
    expect(router.state.matches.at(-1)?.routeId).toBe("/app/settings/$section")
  })
```

If the file names its mock helper differently, use that name.

- [ ] **Step 2: Run it to see it fail**

Run: `cd web && timeout 300 npx vitest run src/router.test.tsx`
Expected: FAIL: no heading "Settings" (the path matches no route).

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

- [ ] **Step 4: Run the checks**

Run: `cd web && timeout 300 npx vitest run src/router.test.tsx && timeout 300 npm run typecheck`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add web/src/router.tsx web/src/router.test.tsx
git commit -m "feat(web): route settings and repository sections"
```

### Task 10: Dashboard

**Files:**
- Modify: `web/src/components/stat-card.tsx` (`StatCard`, `StatCards`)
- Modify: `web/src/components/repo-table.tsx` (`STATE_CLASS` and the state cell)
- Modify: `web/src/components/activity-panel.tsx` (`Legend`), `web/src/components/lanes-chart.tsx` (run and runner state classes)
- Test: `web/src/components/stat-card.test.tsx`, `web/src/components/repo-table.test.tsx`, `web/src/components/activity-panel.test.tsx`, `web/src/components/lanes-chart.test.tsx`

**Interfaces:**
- Consumes: `StatusPill` (Contracts, "Status"); the `running` colour (Contracts, "Tokens and classes"); `Section` (Contracts, "Box").
- Produces: nothing other tasks use.

**Items:** 1

**Depends on:** Task 4, Task 5

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 1 - coupling 1 - risk 1 = 4

- [ ] **Step 1: Add the failing tests**

In `web/src/components/stat-card.test.tsx`, with the file's existing fixtures for `StatCards`:

```tsx
  it("holds the four stats in one box split by dividers", () => {
    const { container } = render(<StatCards status={fixtures.status} metrics={fixtures.metrics} now={Date.now()} />)
    const box = container.firstElementChild as HTMLElement
    expect(box).toHaveClass("rounded-md", "border", "bg-card")
    const stats = within(box).getAllByRole("region")
    expect(stats).toHaveLength(4)
    for (const stat of stats) expect(stat).not.toHaveClass("border", "bg-card")
  })
```

In `web/src/components/repo-table.test.tsx`, in the test that reads a running repository's state:

```tsx
    expect(within(row).getByText("Running").closest("span[class*='rounded-full']")).not.toBeNull()
```

Run: `cd web && timeout 300 npx vitest run src/components/stat-card.test.tsx src/components/repo-table.test.tsx`
Expected: FAIL on both new assertions.

- [ ] **Step 2: One box for the stats, in `web/src/components/stat-card.tsx`**

Replace `StatCard`'s `<section>` class and the `StatCards` wrapper class:

```tsx
    <section aria-label={label} className="flex min-w-0 flex-col gap-1 p-3">
```

```tsx
    <div className="grid grid-cols-2 divide-border rounded-md border border-border bg-card min-[641px]:grid-cols-4 min-[641px]:divide-x">
```

In the same file change the value's `font-medium` to `font-semibold`. The four `StatCard` uses, the meter and the sparklines stay; the "Waiting jobs" sparkline keeps `text-warning`.

- [ ] **Step 3: Pills in the repository table, in `web/src/components/repo-table.tsx`**

Delete `STATE_CLASS`. Where the state cell renders `repoStateWord(repo)` inside a `<span>` with a `STATE_CLASS` class, render instead:

```tsx
<StatusPill state={repoStateWord(repo).toLowerCase()} />
```

with `import { StatusPill } from "@/components/page/status-pill"`.

- [ ] **Step 4: Status colours in the lanes**

In `web/src/components/activity-panel.tsx`, in `Legend`'s lane items replace these three entries and the CPU entry:

```tsx
        ["Running", <Swatch className="fill-running stroke-running" />],
        ["Finishing", <Swatch className="fill-running/25 stroke-running/60" />],
        ["Warm", <Swatch className="fill-none stroke-running" dash />],
```

```tsx
        ["CPU", <LineSwatch className="stroke-foreground" />],
```

and in the bucket items:

```tsx
        ["Average CPU", <LineSwatch className="stroke-foreground" />],
```

In `web/src/components/lanes-chart.tsx` and `web/src/components/buckets-chart.tsx` is not touched. In `lanes-chart.tsx`, find every class that draws a running, finishing or warm bar with `primary` (`rg -n "primary" src/components/lanes-chart.tsx`) and change `primary` to `running` in those; change the CPU line's `stroke-primary` to `stroke-foreground`. Leave focus rings and the "now" marker on `primary`.

Update `activity-panel.test.tsx` and `lanes-chart.test.tsx` where they assert one of the changed classes.

- [ ] **Step 5: Run the checks**

Run: `cd web && timeout 600 npm test && timeout 300 npm run typecheck && timeout 300 npm run lint`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add web/src/components/stat-card.tsx web/src/components/stat-card.test.tsx web/src/components/repo-table.tsx web/src/components/repo-table.test.tsx web/src/components/activity-panel.tsx web/src/components/activity-panel.test.tsx web/src/components/lanes-chart.tsx web/src/components/lanes-chart.test.tsx
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

With the file's existing render helper and a fixture list with one selected runner:

```tsx
  it("marks the selected runner with the sunk fill and an accent edge", () => {
    renderList({ selected: fixtures.status.instances[0]?.id })
    const current = screen.getAllByRole("link").find((a) => a.className.includes("bg-muted "))
    expect(current).toBeDefined()
    expect(current).toHaveClass("shadow-[inset_2px_0_0_hsl(var(--primary))]")
  })
```

If rows are buttons or list items in this file, query the role the file's other tests use.

Run: `cd web && timeout 300 npx vitest run src/components/runner-list.test.tsx`
Expected: FAIL: the class is missing.

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
Expected: PASS. An assertion expecting `bg-primary` on a busy light now expects `bg-running`.

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

Using the file's existing setup (fixture actions, `setViewport(1024)` for wide):

```tsx
  it("lists every run in one box with the count and the filters in its header", async () => {
    renderActions()
    const box = await screen.findByRole("region", { name: /workflow runs?$/ })
    expect(within(box).getByRole("combobox", { name: "Repository" })).toBeInTheDocument()
    expect(within(box).getByRole("radiogroup", { name: "Status" })).toBeInTheDocument()
    expect(within(box).getAllByRole("table")).toHaveLength(1)
    expect(screen.queryByRole("region", { name: "Recent" })).not.toBeInTheDocument()
  })

  it("heads the active runs with In progress and each day with its label", async () => {
    renderActions()
    const box = await screen.findByRole("region", { name: /workflow runs?$/ })
    const headings = within(box).getAllByRole("rowheader").map((h) => h.textContent)
    expect(headings[0]).toBe("In progress")
    expect(headings.length).toBeGreaterThan(1)
  })

  it("writes the facts line as workflow and run number, repository, event and actor", async () => {
    renderActions()
    const run = fixtures.actions.runs[0]!
    const row = (await screen.findByRole("link", { name: new RegExp(run.title) })).closest("tr") as HTMLElement
    expect(within(row).getByText(`${run.workflow} #${run.run_number}`)).toBeInTheDocument()
    expect(within(row).getByText(run.repo)).toBeInTheDocument()
  })
```

Name `renderActions` after the helper the file already uses; if the fixture has no active run, the second test expects the first heading to be a day label and drops the "In progress" line.

Run: `cd web && timeout 300 npx vitest run src/pages/actions.test.tsx`
Expected: FAIL: no region named "… workflow runs".

- [ ] **Step 2: Replace the row and delete `RunHead` in `web/src/pages/actions.tsx`**

Rename the local `RunRow` to `ActionRow` and write it on the shared row. Import `RunGroup, RunRow, runCols, RunValue` from `@/components/page/run-row`, and drop the `Table*` imports that become unused.

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

Move the `Select` and the `ToggleGroup` out of the standalone filter `<div>` into a `filters` constant, and render the lists as one flush section. Replace the final `else` branch of `body` with:

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
Expected: PASS. Older assertions that look for the regions "In progress" or "Recent", or for column headers "Run", "Duration", "Started", change to the row headers and the single region.

- [ ] **Step 5: Commit**

```bash
git add web/src/pages/actions.tsx web/src/pages/actions.test.tsx
git commit -m "feat(web): list workflow runs as rows in one box"
```

### Task 13: History

**Files:**
- Modify: `web/src/pages/history.tsx` (`HistoryRow`, the table branch of `body`, the "Finished jobs" section)
- Test: `web/src/pages/history.test.tsx`

**Interfaces:**
- Consumes: `RunRow`, `RunValue`, `RunGroup`, `runCols` (Contracts, "Rows and side layout"); `Section` with `flush` (Contracts, "Box").
- Produces: nothing other tasks use.

**Items:** 5

**Depends on:** Task 5, Task 8

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 1 - coupling 1 - risk 1 = 4

- [ ] **Step 1: Add the failing tests to `web/src/pages/history.test.tsx`**

Using the file's existing setup:

```tsx
  it("links a finished job to its run page", async () => {
    renderHistory()
    const entry = fixtures.history.find((h) => h.run_id > 0)!
    const link = (await screen.findAllByRole("link", { name: new RegExp(entry.job_name) }))[0]!
    expect(link).toHaveAttribute("href", `/actions/${entry.repo}/runs/${entry.run_id}`)
  })

  it("heads the list with the summary and writes workflow, repository and run number as facts", async () => {
    renderHistory()
    const box = await screen.findByRole("region", { name: "Finished jobs" })
    const entry = fixtures.history[0]!
    const row = within(box).getAllByRole("row").find((r) => r.textContent?.includes(entry.job_name)) as HTMLElement
    expect(within(row).getByText(entry.workflow)).toBeInTheDocument()
    expect(within(row).getByText(`#${entry.run_number}`)).toBeInTheDocument()
    expect(within(box).queryByRole("columnheader", { name: "Repository" })).not.toBeInTheDocument()
  })
```

Name `renderHistory` after the helper the file already uses.

Run: `cd web && timeout 300 npx vitest run src/pages/history.test.tsx`
Expected: FAIL: no link named after the job.

- [ ] **Step 2: Rewrite `HistoryRow` in `web/src/pages/history.tsx`**

Keep the two existing icon buttons exactly as they are (the `Tooltip` blocks for "Open run" and "Copy run URL"), moved into `actions`. Import `Link` from `@tanstack/react-router` if it is not imported, and `RunGroup, RunRow, runCols, RunValue` from `@/components/page/run-row`.

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
          <RunValue kind="duration">
            {dur(ms)}
            <span aria-hidden="true" className="h-1.5 w-16 rounded-[2px] bg-muted">
              <span className="block h-1.5 rounded-[2px] bg-foreground/40" style={{ width: `${Math.round((ms / longest) * 100)}%` }} />
            </span>
          </RunValue>
        </>
      }
      actions={
        url ? (
          <span className="flex justify-end gap-1">{/* the two existing Tooltip + Button blocks, unchanged, using `name` and `url` */}</span>
        ) : (
          <span />
        )
      }
    />
  )
}
```

Replace the comment with the two existing `Tooltip` blocks from the old last cell.

- [ ] **Step 3: Rewrite the table branch and the section**

Add `const wide = useMediaQuery("(min-width: 768px)")` (from `@/lib/use-media-query`) in `HistoryPage`. Replace the last `else` branch of `body` with:

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

Replace the "Finished jobs" `Section` with a flush one whose header carries the summary, and pad the lines that are not the table:

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

The `PageHeader` keeps its `description`. Drop the `Table*` imports that become unused. The controls row and the "Jobs" chart section stay as they are.

- [ ] **Step 4: Run the tests**

Run: `cd web && timeout 300 npx vitest run src/pages/history.test.tsx && timeout 300 npm run typecheck && timeout 300 npm run lint`
Expected: PASS. Older assertions on the column headers Finished, Repository, Job, Run, Result and Duration are removed; assertions on day headings, the pick line, the 500-row note and the empty states stay.

- [ ] **Step 5: Commit**

```bash
git add web/src/pages/history.tsx web/src/pages/history.test.tsx
git commit -m "feat(web): list finished jobs as rows"
```

### Task 14: Run page

**Files:**
- Modify: `web/src/components/run-frame.tsx` (`RAIL_LINK`, `RAIL_LABEL`, the `PageHeader`)
- Modify: `web/src/components/run-graph.tsx` (the graph canvas and the job tiles)
- Test: `web/src/components/run-frame.test.tsx`, `web/src/components/run-graph.test.tsx`

**Interfaces:**
- Consumes: `StatusPill` (Contracts, "Status"); `SIDE_LINK`, `SideGroup` (Contracts, "Rows and side layout").
- Produces: nothing other tasks use.

**Items:** 6

**Depends on:** Task 4, Task 8

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 1 - coupling 1 - risk 1 = 4

- [ ] **Step 1: Add the failing tests**

In `web/src/components/run-frame.test.tsx`, with the file's existing render helper and `fixtures.runDetail`:

```tsx
  it("shows the run's state as a pill beside the title", () => {
    renderFrame()
    const run = fixtures.runDetail
    const word = run.status === "completed" ? resultWord(run.conclusion) : run.status === "in_progress" ? "Running" : "Waiting"
    const pill = screen.getAllByText(word).find((el) => el.parentElement?.className.includes("rounded-full"))
    expect(pill).toBeDefined()
  })

  it("marks the current entry of the job list with the side-link style", () => {
    renderFrame()
    const summary = screen.getByRole("link", { name: "Summary" })
    expect(summary.className).toContain("aria-[current=page]:shadow-[inset_2px_0_0_hsl(var(--primary))]")
  })
```

Import `resultWord` from `@/lib/history`.

In `web/src/components/run-graph.test.tsx`, with the file's existing render helper:

```tsx
  it("draws the jobs on a ground canvas", () => {
    renderGraph()
    expect(screen.getByRole("group", { name: "Job graph" })).toHaveClass("bg-background", "rounded-md")
  })
```

Run: `cd web && timeout 300 npx vitest run src/components/run-frame.test.tsx src/components/run-graph.test.tsx`
Expected: FAIL on all three.

- [ ] **Step 2: Check the header's title type**

Run: `cd web && rg -n "title" node_modules/darkraise-ui/dist/layout/page-header/*.d.ts | head`
Expected: the `title` prop's type. If it is `ReactNode`, use variant A in Step 3; if it is `string`, use variant B.

- [ ] **Step 3: Change `web/src/components/run-frame.tsx`**

Replace the two class constants and add the pill:

```tsx
import { StatusPill } from "@/components/page/status-pill"
import { SIDE_LINK, SideGroup } from "@/components/page/side-layout"
import { resultWord } from "@/lib/history"
import { isRunActive } from "@/lib/run-details"

const RAIL_LINK = SIDE_LINK

function RunPill({ run }: { run: RunDetail }) {
  if (!isRunActive(run.status)) return <StatusPill state={run.conclusion} label={resultWord(run.conclusion)} />
  return run.status === "in_progress" ? <StatusPill state="running" /> : <StatusPill state="queued" label="Waiting" />
}
```

Replace each `<p className={RAIL_LABEL}>…</p>` with `<SideGroup>…</SideGroup>` and delete `RAIL_LABEL`.

Variant A (`title` is `ReactNode`):

```tsx
      <PageHeader
        breadcrumbs={breadcrumbs}
        title={
          <span className="flex flex-wrap items-center gap-2">
            <RunPill run={run} />
            {run.title}
          </span>
        }
        description={runDescription(run)}
        actions={actions}
      />
```

Variant B (`title` is `string`): keep `title={run.title}` and put the pill first in the actions:

```tsx
  const actions = (
    <div className="flex flex-wrap items-center gap-2">
      <RunPill run={run} />
      {/* the attempt Select and the Open on GitHub button, unchanged */}
    </div>
  )
```

- [ ] **Step 4: Change `web/src/components/run-graph.tsx`**

On the element with `role="group" aria-label="Job graph"`, replace the class with:

```tsx
className="overflow-x-auto rounded-md bg-background p-3"
```

On the connectors' `<svg>`, change `text-border` to `text-muted-foreground` and the paths' `strokeWidth={1.5}` to `strokeWidth={1}`.

On both tile wrappers (the single-job tile's link and the matrix tile's `div` with `rounded-[6px] border border-border bg-card`), use `rounded-md border border-border bg-card` and add `hover:border-primary`; where a tile is the current job, add `border-primary`.

- [ ] **Step 5: Run the checks**

Run: `cd web && timeout 300 npx vitest run src/components/run-frame.test.tsx src/components/run-graph.test.tsx src/pages/run.test.tsx && timeout 300 npm run typecheck && timeout 300 npm run lint`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add web/src/components/run-frame.tsx web/src/components/run-frame.test.tsx web/src/components/run-graph.tsx web/src/components/run-graph.test.tsx
git add -u web/src/pages/run.test.tsx
git commit -m "feat(web): redraw the run page in Forge"
```

### Task 15: Job log panel

**Files:**
- Modify: `web/src/components/log-viewer.tsx` (`LogViewer`'s props and returned markup, `renderLines`' wrapper)
- Modify: `web/src/pages/run.tsx` (`JobLogs`, where it renders `LogViewer`)
- Modify: `web/src/styles/ghr-theme.css` (after the `.ghr-log` rules)
- Test: `web/src/components/log-viewer.test.tsx`

**Interfaces:**
- Consumes: nothing from other tasks.
- Produces: `LogViewer` gains `heading?: ReactNode`; nothing other tasks use.

**Items:** 6

**Depends on:** Task 2

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 1 - coupling 1 - risk 1 = 4

- [ ] **Step 1: Add the failing test to `web/src/components/log-viewer.test.tsx`**

With the file's existing render helper (pass `heading` through it):

```tsx
  it("holds the heading, the search box and the steps in one dark panel", () => {
    renderViewer({ heading: <span>lint</span> })
    const panel = screen.getByRole("list", { name: "Steps" }).closest(".ghr-log-panel") as HTMLElement
    expect(panel).not.toBeNull()
    expect(within(panel).getByText("lint")).toBeInTheDocument()
    expect(within(panel).getByRole("searchbox", { name: "Search logs" })).toBeInTheDocument()
  })
```

If the search input has the role `textbox` in this file's other tests, query that role.

Run: `cd web && timeout 300 npx vitest run src/components/log-viewer.test.tsx`
Expected: FAIL: no `.ghr-log-panel` ancestor.

- [ ] **Step 2: Add the panel's tokens to `web/src/styles/ghr-theme.css`**

After the `.ghr-log { … }` rule:

```css
/* Kit controls inside the panel read the page tokens, so the panel rebinds
   them to its dark palette in both modes. */
.ghr-log-panel {
  --background: var(--log-bg);
  --card: var(--log-bg);
  --foreground: var(--log-fg);
  --muted: 217.5 17.4% 18.0%;
  --muted-foreground: var(--log-muted);
  --border: 213.8 11.6% 27.1%;
  --input: 213.8 11.6% 27.1%;
  --destructive: 2.7 92.6% 62.9%;
  --warning: 23.1 71.3% 50.8%;
  --success: 128.4 49.2% 48.6%;
  --running: 40.6 72.1% 47.8%;
  background: hsl(var(--log-bg));
  color: hsl(var(--log-fg));
}

.ghr-log-error {
  box-shadow: inset 2px 0 0 hsl(var(--destructive));
}

.ghr-log-warning {
  box-shadow: inset 2px 0 0 hsl(var(--warning));
}
```

The two marker rules add to the existing `.ghr-log-error` and `.ghr-log-warning` rules; do not remove those.

- [ ] **Step 3: Wrap the viewer in the panel, in `web/src/components/log-viewer.tsx`**

Add `heading?: ReactNode` to `LogViewer`'s props. Replace the returned outer structure so the toolbar and the steps share one panel:

```tsx
    <div ref={ref} className="ghr-log-panel overflow-hidden rounded-md border border-border">
      <div className="flex flex-wrap items-center gap-3 border-b border-border px-3 py-2">
        {heading && <div className="mr-auto flex min-w-0 items-center gap-2 text-sm font-semibold">{heading}</div>}
        {/* the existing search Input and its "{i} of {n}" readout, unchanged */}
      </div>
      <ol aria-label="Steps" className="flex flex-col">
        {/* the existing step items, unchanged except for the two class edits below */}
      </ol>
    </div>
```

In each step's button, change `py-1.5` to `px-3 py-1.5`. In `renderLines`, change the wrapper class from `ghr-log overflow-x-auto rounded-md py-2 font-mono text-xs leading-5` to `ghr-log overflow-x-auto py-2 font-mono text-xs leading-5`, and the surrounding `<div className="pb-2">` to `<div>`.

- [ ] **Step 4: Pass the job as the heading, in `web/src/pages/run.tsx`**

In `JobLogs`, where `<LogViewer` is rendered, add:

```tsx
      heading={
        <>
          <RunStatusIcon status={job.status} conclusion={job.conclusion} />
          <span className="min-w-0 truncate">{job.name}</span>
        </>
      }
```

Import `RunStatusIcon` from `@/components/run-status-icon` if `run.tsx` does not already.

- [ ] **Step 5: Run the checks**

Run: `cd web && timeout 300 npx vitest run src/components/log-viewer.test.tsx src/pages/run.test.tsx && timeout 300 npm run typecheck && timeout 300 npm run lint`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add web/src/components/log-viewer.tsx web/src/components/log-viewer.test.tsx web/src/pages/run.tsx web/src/styles/ghr-theme.css
git commit -m "feat(web): hold a job's steps and log in one dark panel"
```

### Task 16: Repositories list

**Files:**
- Create: `web/src/components/repo-list.tsx`
- Test: `web/src/components/repo-list.test.tsx`
- Modify: `web/src/pages/repositories.tsx` (where it renders `RepoTable columns="full"`), `web/src/pages/repositories.test.tsx`
- Modify: `web/src/components/repo-table.tsx` (export `RowMenu`; delete the `columns` prop and the `"full"` branch), `web/src/components/repo-table.test.tsx`

**Interfaces:**
- Consumes: `StatusPill` (Contracts, "Status"); `Section` with `flush` (Contracts, "Box").
- Produces: nothing other tasks use.

**Items:** 7

**Depends on:** Task 4, Task 5, Task 10

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 1 - coupling 1 - risk 1 = 4

- [ ] **Step 1: Read what the full table shows**

Run: `cd web && rg -n "full|Warm|weekRate|weekText|RepoActivityStrip|labels" src/components/repo-table.tsx`
Note the exact expressions the `"full"` branch uses for the Warm cell, the 7-days cell (`weekRate`, `weekText` and the visually hidden counts), the Labels cell, the `RepoActivityStrip` props, and `RowMenu`'s props. Step 3 reuses them verbatim.

- [ ] **Step 2: Write the failing test `web/src/components/repo-list.test.tsx`**

```tsx
import { screen, within } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import { fixtures } from "@/test/fixtures"
import { renderRoutes } from "@/test/routes"
import { RepoList } from "./repo-list"

function renderList() {
  return renderRoutes(
    {
      "/": () => (
        <RepoList
          repos={fixtures.status.repos}
          config={fixtures.config}
          activity={fixtures.activityBuckets.repos}
          mode={fixtures.status.mode}
          now={Date.now()}
          offline={false}
          onOpen={() => {}}
        />
      ),
      "/repositories/$name": () => null,
    },
    "/",
  )
}

describe("RepoList", () => {
  it("shows each repository as a row with its name as a link and its state as a pill", async () => {
    renderList()
    const repo = fixtures.status.repos[0]!
    const link = await screen.findByRole("link", { name: repo.name })
    expect(link).toHaveAttribute("href", `/repositories/${repo.name}`)
    const row = link.closest("li") as HTMLElement
    expect(row.querySelector("span[class*='rounded-full']")).not.toBeNull()
    expect(within(row).getByRole("button", { name: `More actions for ${repo.name}` })).toBeInTheDocument()
  })

  it("lists one row per repository", async () => {
    renderList()
    await screen.findByRole("link", { name: fixtures.status.repos[0]!.name })
    expect(screen.getAllByRole("listitem")).toHaveLength(fixtures.status.repos.length)
  })
})
```

Run: `cd web && timeout 300 npx vitest run src/components/repo-list.test.tsx`
Expected: FAIL, cannot resolve `./repo-list`.

- [ ] **Step 3: Write `web/src/components/repo-list.tsx`**

Export `RowMenu` from `repo-table.tsx` first. Then:

```tsx
import { Link } from "@tanstack/react-router"
import type { ActivityRepo, Config, RepoStatus } from "@/api/types"
import { StatusPill } from "@/components/page/status-pill"
import { RepoActivityStrip } from "@/components/repo-activity-strip"
import { RowMenu } from "@/components/repo-table"
import { RunnerMeter } from "@/components/runner-meter"
import { repoStateWord } from "@/lib/status"

const LABEL = "rounded-full bg-muted px-2 py-0.5 font-mono text-xs text-foreground"

export function RepoList({
  repos,
  config,
  activity,
  mode,
  now,
  offline,
  onOpen,
}: {
  repos: RepoStatus[]
  config: Config | undefined
  activity: ActivityRepo[] | undefined
  mode: string
  now: number
  offline: boolean
  onOpen: (name: string) => void
}) {
  return (
    <ul className="flex flex-col divide-y divide-border">
      {repos.map((repo) => {
        const cfg = config?.repos?.find((r) => r.name === repo.name)
        const week = activity?.find((a) => a.name === repo.name)
        return (
          <li key={repo.name} className="flex flex-wrap items-start gap-x-6 gap-y-2 px-4 py-3 hover:bg-muted/50">
            <div className="flex min-w-0 flex-1 flex-col gap-1.5">
              <span className="flex flex-wrap items-center gap-2">
                <Link to="/repositories/$name" params={{ name: repo.name }} className="text-sm font-semibold text-primary hover:underline">
                  {repo.name}
                </Link>
                <StatusPill state={repoStateWord(repo).toLowerCase()} />
              </span>
              {repo.removing ? (
                <span className="text-xs text-muted-foreground">Removing. Running jobs finish first.</span>
              ) : (
                <span className="flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-muted-foreground">
                  {(cfg?.labels ?? []).map((l) => (
                    <span key={l} className={LABEL}>
                      {l}
                    </span>
                  ))}
                  {mode === "all" && cfg && <span>{/* the Warm cell's expression from the full table, prefixed "Warm " */}</span>}
                  {week && <span>{/* the 7-days cell's expression and its sr-only counts, followed by " succeeded in 7 days" */}</span>}
                </span>
              )}
            </div>
            <div className="flex items-center gap-4">
              <span className="flex items-center gap-2 font-mono text-sm">
                {repo.max === 0 ? repo.active : `${repo.active}/${repo.max}`}
                {repo.max > 0 && <RunnerMeter active={repo.active} max={repo.max} />}
              </span>
              {repo.queued > 0 && <span className="font-mono text-sm text-warning">{`${repo.queued} waiting`}</span>}
              <span className="hidden md:block">{/* <RepoActivityStrip … /> with the props the table passes */}</span>
              <RowMenu repo={repo} offline={offline} onOpen={onOpen} />
            </div>
          </li>
        )
      })}
    </ul>
  )
}
```

Fill the three commented slots with the expressions noted in Step 1; if the table reads the labels, warm count or week counts from differently named fields than `cfg.labels` and `week`, use the table's own lookups. Render a slot's `<span>` only when the table renders a non-blank cell.

- [ ] **Step 4: Use it on the page and delete the full table**

In `web/src/pages/repositories.tsx`, replace the block that renders `<RepoTable … columns="full" />` (and its `overflow-x-auto` wrapper) with one flush section, and move the summary and the Add button into its header:

```tsx
        <Section
          title="Repositories"
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
            mode={st.mode}
            now={now}
            offline={offline}
            onOpen={(name) => void navigate({ to: "/repositories/$name", params: { name } })}
          />
        </Section>
```

The `PageHeader` keeps its title and loses `description` and `actions` only when the list is shown; the empty state keeps its own Add button. In `web/src/components/repo-table.tsx`, delete the `columns` prop and everything guarded by `full`, and delete the matching cases in `repo-table.test.tsx`. In `repositories.test.tsx`, assertions on the Warm, 7 days and Labels column headers become assertions on the row's facts line; the rest stay.

- [ ] **Step 5: Run the checks**

Run: `cd web && timeout 300 npx vitest run src/components/repo-list.test.tsx src/components/repo-table.test.tsx src/pages/repositories.test.tsx src/pages/dashboard.test.tsx && timeout 300 npm run typecheck && timeout 300 npm run lint`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add web/src/components/repo-list.tsx web/src/components/repo-list.test.tsx web/src/pages/repositories.tsx web/src/pages/repositories.test.tsx web/src/components/repo-table.tsx web/src/components/repo-table.test.tsx
git commit -m "feat(web): list repositories as rows"
```

### Task 17: Repository page sections

**Files:**
- Modify: `web/src/pages/repository.tsx` (`RepositoryPage`, `SECTIONS`, the returned layout of `RepositoryForm`)
- Test: `web/src/pages/repository.test.tsx`

**Interfaces:**
- Consumes: `/repositories/$name/$section` (Contracts, "Routes"); `SideLayout`, `SIDE_LINK` (Contracts, "Rows and side layout"); `StatusPill` (Contracts, "Status").
- Produces: nothing other tasks use.

**Items:** 7

**Depends on:** Task 4, Task 8, Task 9

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 1 - coupling 1 - risk 1 = 4

- [ ] **Step 1: Add the failing tests to `web/src/pages/repository.test.tsx`**

Using the file's existing mocks, and `renderApp` from `@/test/render`:

```tsx
  it("opens on the overview and keeps the form on its own section", async () => {
    mockApi(authedRoutes())
    renderApp("/repositories/darkmem")
    const nav = await screen.findByRole("navigation", { name: "Repository" })
    expect(within(nav).getByRole("link", { name: "Overview" })).toHaveAttribute("aria-current", "page")
    expect(screen.queryByRole("heading", { name: "Capacity" })).not.toBeInTheDocument()
  })

  it.each([
    ["settings", "Capacity and labels", "Capacity"],
    ["labels", "Workflow labels", "Workflow labels"],
    ["registrations", "GitHub registrations", "GitHub registrations"],
  ])("shows the %s section at its own address", async (section, link, heading) => {
    mockApi(authedRoutes())
    renderApp(`/repositories/darkmem/${section}`)
    const nav = await screen.findByRole("navigation", { name: "Repository" })
    expect(within(nav).getByRole("link", { name: link })).toHaveAttribute("aria-current", "page")
    expect(await screen.findByRole("heading", { name: heading })).toBeInTheDocument()
  })

  it("says so when the section does not exist", async () => {
    mockApi(authedRoutes())
    renderApp("/repositories/darkmem/nope")
    expect(await screen.findByText("This repository has no such section.")).toBeInTheDocument()
  })
```

Use the repository name and mock helper the file's other tests use, and add the label-check and registrations routes its other tests already mock.

Run: `cd web && timeout 300 npx vitest run src/pages/repository.test.tsx`
Expected: FAIL: no navigation named "Repository".

- [ ] **Step 2: Read the section from the route, in `web/src/pages/repository.tsx`**

```tsx
const SECTIONS = [
  { id: "", title: "Overview" },
  { id: "settings", title: "Capacity and labels" },
  { id: "labels", title: "Workflow labels" },
  { id: "registrations", title: "GitHub registrations" },
] as const

export function RepositoryPage() {
  const { name = "", section = "" } = useParams({ strict: false })
  // Keyed by name: each repository starts with its own empty draft.
  return <RepositoryForm key={name} name={name} section={section} />
}
```

`RepositoryForm` takes `section: string`. Delete the `SectionNav` import.

- [ ] **Step 3: Lay the form out in sections**

Replace the returned layout of `RepositoryForm` from `<RepoActivity …/>` down to the closing `</div>` before `<SaveBar`, keeping the `PageHeader`, the "Being removed" line and the rejected alert above it:

```tsx
      <SideLayout
        label="Repository"
        nav={SECTIONS.map((s) =>
          s.id === "" ? (
            <Link key={s.id} to="/repositories/$name" params={{ name }} activeOptions={{ exact: true }} className={SIDE_LINK}>
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
        {section === "settings" && (
          <div className="flex min-w-0 flex-col gap-6">
            {/* the existing Capacity, Labels and Cleanup FieldGroups, unchanged */}
          </div>
        )}
        {section === "labels" && (
          <LabelCheckGroup name={name} effective={effective} degraded={degraded} degradedReason={status.data?.degraded_reason} disabled={locked} onAddLabel={addLabel} />
        )}
        {section === "registrations" && (
          <RegistrationsGroup name={name} degraded={degraded} degradedReason={status.data?.degraded_reason} disabled={locked} />
        )}
        {!SECTIONS.some((s) => s.id === section) && <p className="text-sm text-muted-foreground">This repository has no such section.</p>}
      </SideLayout>
```

Move the three existing `FieldGroup`s (Capacity, Labels, Cleanup) into the `settings` branch unchanged. `SaveBar` and `UnsavedGuard` stay after the layout, unchanged, so a draft made on "Capacity and labels" is guarded on any navigation. In `addLabel`, after `setDraft`, nothing changes: the label joins the draft and the save bar shows it.

In the `PageHeader`, show the state as a pill ahead of the state sentence:

```tsx
        description={
          repoStatus ? (
            <span className="flex flex-wrap items-center gap-2">
              <StatusPill state={repoStateWord(repoStatus).toLowerCase()} />
              {repoDescription(repoStatus)}
            </span>
          ) : undefined
        }
```

If the kit's `description` prop is typed `string` (the typecheck will say), keep `description={repoDescription(repoStatus)}` and render the pill as the first child of `HeaderActions`' wrapper instead.

- [ ] **Step 4: Run the checks**

Run: `cd web && timeout 300 npx vitest run src/pages/repository.test.tsx && timeout 300 npm run typecheck && timeout 300 npm run lint`
Expected: PASS. Older tests that open `/repositories/<name>` and then use the form, the label check or the registrations open the matching section address instead.

- [ ] **Step 5: Commit**

```bash
git add web/src/pages/repository.tsx web/src/pages/repository.test.tsx
git commit -m "feat(web): split the repository page into sections"
```

### Task 18: Settings area

**Files:**
- Modify: `web/src/pages/settings.tsx`
- Modify: `web/src/pages/toolchains.tsx`, `web/src/pages/storage.tsx` (no change to their bodies unless Step 4 says so)
- Test: `web/src/pages/settings.test.tsx`, `web/src/pages/toolchains.test.tsx`, `web/src/pages/storage.test.tsx`

**Interfaces:**
- Consumes: `/settings/$section` (Contracts, "Routes"); `SideLayout`, `SideGroup`, `SIDE_LINK` (Contracts, "Rows and side layout").
- Produces: nothing other tasks use.

**Items:** 8

**Depends on:** Task 8, Task 9

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 1 - coupling 1 - risk 1 = 4

- [ ] **Step 1: Add the failing tests to `web/src/pages/settings.test.tsx`**

Using the file's existing mocks and `renderApp`:

```tsx
  it("opens on General with the section list beside it", async () => {
    mockApi(authedRoutes())
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
    expect(within(nav).getByRole("link", { name: "General" })).toHaveAttribute("aria-current", "page")
    expect(await screen.findByRole("heading", { level: 1, name: "General" })).toBeInTheDocument()
    expect(screen.queryByRole("heading", { name: "GitHub token" })).not.toBeInTheDocument()
  })

  it.each([
    ["token", "GitHub token"],
    ["runner", "Runner software"],
    ["account", "Account"],
    ["toolchains", "Toolchains"],
    ["storage", "Storage"],
  ])("shows the %s section at its own address", async (section, title) => {
    mockApi(authedRoutes())
    renderApp(`/settings/${section}`)
    const nav = await screen.findByRole("navigation", { name: "Settings" })
    expect(within(nav).getByRole("link", { name: title })).toHaveAttribute("aria-current", "page")
    expect(await screen.findByRole("heading", { level: 1, name: title })).toBeInTheDocument()
  })

  it("says so when the section does not exist", async () => {
    mockApi(authedRoutes())
    renderApp("/settings/nope")
    expect(await screen.findByText("Settings has no such section.")).toBeInTheDocument()
  })
```

Add the token route and any others the file's existing tests mock for those sections.

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
import { SIDE_LINK, SideGroup, SideLayout } from "@/components/page/side-layout"
import { RunnerSection } from "@/components/runner-section"
import { SaveBar } from "@/components/save-bar"
import { SettingsSections } from "@/components/settings-form"
import { TokenSection } from "@/components/token-section"
import { UnsavedGuard } from "@/components/unsaved-guard"
import { useSettingsForm } from "@/lib/settings-form"
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

function content(section: string): ReactNode {
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
      {content(section)}
    </SideLayout>
  )
}
```

- [ ] **Step 3: Keep one heading per section**

`TokenSection`, `RunnerSection` and `AccountSection` are `FieldGroup`s with their own `h2` ("GitHub token", "Runner and config", "Account"). Run `cd web && rg -n "FieldGroup" src/components/token-section.tsx src/components/runner-section.tsx src/components/account-section.tsx`. In `runner-section.tsx` change the group's title from "Runner and config" to "Runner software"; leave the other titles. The `h1` and `h2` then read the same on those three sections, which the heading order allows.

- [ ] **Step 4: Fix the two tests that Task 9's test and this layout reach**

- `web/src/router.test.tsx` expects the heading "Settings" at `/settings/token`; change the expected name to "GitHub token".
- `web/src/pages/toolchains.test.tsx` and `web/src/pages/storage.test.tsx`: where a test renders through `renderApp("/toolchains")` or `renderApp("/storage")`, leave it (those routes still serve the pages until Task 19). Add one case to each:

```tsx
  it("is served inside Settings", async () => {
    mockApi(authedRoutes())
    renderApp("/settings/toolchains")
    expect(await screen.findByRole("heading", { level: 1, name: "Toolchains" })).toBeInTheDocument()
    expect(screen.getByRole("navigation", { name: "Settings" })).toBeInTheDocument()
  })
```

(with `storage` and "Storage" in the storage test).

- [ ] **Step 5: Run the checks**

Run: `cd web && timeout 300 npx vitest run src/pages/settings.test.tsx src/pages/toolchains.test.tsx src/pages/storage.test.tsx src/router.test.tsx && timeout 300 npm run typecheck && timeout 300 npm run lint`
Expected: PASS. Older settings tests that open `/settings` and then use the token, runner or account section open that section's address instead; tests of the unsaved guard stay on `/settings`, plus one new case that clicks the "GitHub token" link with a changed field and expects the guard's dialog.

- [ ] **Step 6: Commit**

```bash
git add web/src/pages/settings.tsx web/src/pages/settings.test.tsx web/src/pages/toolchains.test.tsx web/src/pages/storage.test.tsx web/src/router.test.tsx web/src/components/runner-section.tsx
git add -u web/src/components
git commit -m "feat(web): gather settings, toolchains and storage"
```

### Task 19: Redirects and links

**Files:**
- Modify: `web/src/router.tsx` (`toolchainsRoute`, `storageRoute`)
- Modify: `web/src/pages/dashboard.tsx` (the "Storage" link), `web/src/components/repo-table.tsx` (`RowMenu`'s Edit item)
- Test: `web/src/router.test.tsx`, `web/src/pages/dashboard.test.tsx`

**Interfaces:**
- Consumes: Contracts, "Routes".
- Produces: nothing other tasks use.

**Items:** 3, 8

**Depends on:** Task 9, Task 16, Task 17, Task 18

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 1 = 3

- [ ] **Step 1: Add the failing tests**

In `web/src/router.test.tsx`:

```tsx
  it.each([
    ["/toolchains", "/settings/toolchains"],
    ["/storage", "/settings/storage"],
  ])("sends %s to its settings section", async (from, to) => {
    mockApi(authedRoutes())
    const { router } = renderApp(from)
    await waitFor(() => expect(router.state.location.pathname).toBe(to))
  })
```

In `web/src/pages/dashboard.test.tsx`:

```tsx
  it("links the disk box to the storage section", async () => {
    renderDashboard()
    expect(await screen.findByRole("link", { name: "Storage" })).toHaveAttribute("href", "/settings/storage")
  })
```

Use each file's own mock and render helpers; import `waitFor` from `@testing-library/react` if it is missing.

Run: `cd web && timeout 300 npx vitest run src/router.test.tsx src/pages/dashboard.test.tsx`
Expected: FAIL on the three new cases.

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

Remove the `ToolchainsPage` and `StoragePage` imports from `router.tsx` if nothing else in the file uses them.

- [ ] **Step 3: Point the links at the new addresses**

In `web/src/pages/dashboard.tsx`, the disk section's link becomes:

```tsx
                <Link to="/settings/$section" params={{ section: "storage" }} className="ml-auto text-sm text-primary hover:underline">
                  Storage
                </Link>
```

In `RowMenu` (`web/src/components/repo-table.tsx`), the Edit item calls a new prop so the list and the table can send it to the settings section. Change `RowMenu`'s `onOpen` uses for Edit only by giving every caller this handler: in `web/src/pages/dashboard.tsx` and `web/src/pages/repositories.tsx`, pass

```tsx
onOpen={(name) => void navigate({ to: "/repositories/$name/$section", params: { name, section: "settings" } })}
```

where `onOpen` feeds `RowMenu`'s Edit. If `RepoTable` also uses `onOpen` for a click on the row, add a second prop `onEdit` to `RepoTable`, `RepoList` and `RowMenu`, pass the handler above as `onEdit`, and keep `onOpen` (to `/repositories/$name`) for the row click.

Then search for other in-app references: `cd web && rg -n '"/toolchains"|"/storage"|to="/toolchains"|to="/storage"' src --glob '!*.test.*'`. Each hit outside `router.tsx` becomes the `/settings/$section` form above.

- [ ] **Step 4: Run the checks**

Run: `cd web && timeout 600 npm test && timeout 300 npm run typecheck && timeout 300 npm run lint`
Expected: PASS. Toolchains and storage tests that render `/toolchains` or `/storage` still pass through the redirect; a test that asserts the pathname after rendering expects the new one.

- [ ] **Step 5: Commit**

```bash
git add web/src/router.tsx web/src/router.test.tsx web/src/pages/dashboard.tsx web/src/pages/dashboard.test.tsx web/src/components/repo-table.tsx
git add -u web/src
git commit -m "feat(web): redirect the old toolchains and storage URLs"
```

### Task 20: Cleanup and release checks

**Files:**
- Delete: `web/src/components/page/state-text.tsx`, `web/src/components/page/state-text.test.tsx`, `web/src/components/page/section-nav.tsx`, `web/src/components/page/section-nav.test.tsx`
- Modify: every file under `web/src` that imports `StateText` (import and tag rename only)
- Modify: `web/src/styles/ghr-theme.css` (remove the unused sidebar tokens)
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
git rm -q src/components/page/state-text.tsx src/components/page/state-text.test.tsx
rg -n "StateText|state-text" src
```

Expected: the last `rg` prints nothing. If a file now imports `StatusPill` twice, merge the two imports.

- [ ] **Step 2: Delete the section index**

```bash
cd web && rg -n "SectionNav|section-nav" src
```

Expected: hits only in `src/components/page/section-nav.tsx` and its test. Then `git rm -q src/components/page/section-nav.tsx src/components/page/section-nav.test.tsx`. If another file still imports it, stop and report the file.

- [ ] **Step 3: Remove what the sidebar left behind**

In `web/src/styles/ghr-theme.css`, delete from both mode blocks the lines for `--surface-sidebar`, `--sidebar-foreground`, `--sidebar-foreground-hover`, `--sidebar-foreground-muted`, `--sidebar-hover-bg` and `--sidebar-border`, after confirming nothing reads them:

```bash
cd web && rg -n "sidebar" src public
```

Expected after the deletion: no hit. A hit in a `.tsx` file means a component still reads a sidebar token; change it to `--muted` (fills) or `--muted-foreground` (text) first.

- [ ] **Step 4: Run every check**

```bash
cd web && timeout 600 npm test && timeout 300 npm run typecheck && timeout 300 npm run lint && timeout 600 npm run build
cd .. && export PATH=$PATH:/usr/local/go/bin && test -z "$(gofmt -l internal cmd web/*.go)" && timeout 300 go vet ./internal/... ./cmd/... ./web/ && timeout 900 go test ./internal/... ./cmd/... ./web/
```

Expected: every command passes. `web/src/a11y.test.tsx` (axe) passes on every page; if it reports a contrast or heading-order violation, fix the page it names and re-run.

- [ ] **Step 5: Check the spec's remaining promises by search**

```bash
cd web && rg -n "IBM Plex|rounded-\[10px\]|font-medium" src --glob '!*.test.*' | head -40
rg -n "bg-primary|text-primary|fill-primary|stroke-primary" src/components/lanes-chart.tsx src/components/capacity-bar.tsx src/components/runner-list.tsx
```

Expected: no `IBM Plex`. Replace each remaining `rounded-[10px]` with `rounded-md` and each remaining `font-medium` with `font-semibold`, then re-run `timeout 600 npm test`. The second command may print only focus-ring or "now" marker classes.

- [ ] **Step 6: Record the register rows**

For each row 1 to 8 of `docs/superpowers/registers/2026-10-10-ghr-forge-ui.md`:

```bash
bash /root/.claude/plugins/cache/darkraise/dr-superpowers/1.33.0/scripts/register set docs/superpowers/registers/2026-10-10-ghr-forge-ui.md <id> verify --note "Automated checks pass; the manual checks of spec section 7 are the owner's, on the LXC."
```

- [ ] **Step 7: Commit**

```bash
git add -A web/src docs/superpowers/registers/2026-10-10-ghr-forge-ui.md
git commit -m "refactor(web): drop the state text alias and section index"
```

- [ ] **Step 8: Report the manual checks**

Do not deploy. Report to the owner that these checks from spec §7 remain, each needing their yes for the deploy to the runner LXC:

- every page in light and in dark, at 1440px and at 400px wide, with no sideways page scroll;
- a real run in progress on the Dashboard, Actions, its run page and its job page;
- a failed run's job page: the first failed step open, the error line marked;
- General and "Capacity and labels": the unsaved guard on a left-list click;
- `/toolchains` and `/storage` land on the new sections;
- a new browser profile opens in the system's mode;
- the tab row: the coral line under the current tab, the divider before Repositories, sideways scroll on a phone.
