# ghr Artifacts page revamp Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: the skill the **Execution:** line names — dr-superpowers:subagent-driven-development for `subagent`, dr-superpowers:executing-plans for `inline`. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Move the GitHub storage policy into a Settings section with the save bar, and make the Artifacts page shorter: quota stat tiles, a filterable repository table that hides empty repositories, one retention line and a totals row.

**Architecture:** Web UI only. A form hook (`lib/storage-policy-form.ts`) backs a new Settings section the way `lib/settings-form.ts` backs General. The Artifacts page keeps its components; the repository table gains a toolbar, sort and footer, the quota card gains tiles from the Dashboard's `StatCard`, and the old Policy card is deleted.

**Tech Stack:** React 19, TypeScript, TanStack Router and Query, darkraise-ui, Tailwind v4, Vitest with Testing Library.

**Spec:** docs/superpowers/specs/2026-10-13-ghr-artifacts-revamp-design.md

**Execution:** inline — `claude --model sonnet --effort medium` — no task is heavy (highest total 4), so the session implements every task itself.

**Plan review:** 2026-10-13 — dr-superpowers:judge-opus — executability 18 / coherence 18 / coverage 18 / assumptions 15 (round 1)

**Parallelism:** sequential — the Settings move and the table changes both edit the Artifacts page and the policy code it imports.

## Global Constraints

- Only `web/src/**` and `README.md` change. No Go, API or config change.
- Run web commands from `web/` with `TMPDIR=/root/.cache/ghr-plan-tmp` (create it with `mkdir -p` first): `/tmp` on this host is a nearly full tmpfs. A single test file: `TMPDIR=/root/.cache/ghr-plan-tmp npx vitest run <path relative to web/>`.
- User-facing texts are copied verbatim from the spec and from this plan's code; do not reword them.
- A component file exports only components (`react-refresh/only-export-components` is a warning and lint runs with `--max-warnings 0`); helpers stay unexported or live in `lib/`.
- Comments only where the reason is not obvious; never describe what the code does or name this task.
- Commits: `<type>(<scope>): <subject>`, subject at most 50 characters, imperative, no period; no attribution lines.

## Contracts

- `web/src/components/cleanup-preview.tsx` exports `CleanupPreview({ age, previews }: { age: string; previews: StoragePreview[] })`, moved unchanged out of `storage-policy-card.tsx` (Task 1). Task 3 deletes `storage-policy-card.tsx` and relies on no other file importing from it.
- The Settings section id `github-storage`, so the route `/settings/github-storage`, titled "GitHub storage" (Task 1). Task 3 links to it as `<Link to="/settings/$section" params={{ section: "github-storage" }}>`.

## Assumptions (evidence)

- darkraise-ui's `Switch` has the `switch` role and takes its name from a `Label htmlFor`: `web/src/components/runner-panel.test.tsx:64`, `web/src/components/add-repo-dialog.test.tsx:130`.
- darkraise-ui exports `TableFooter`: `web/node_modules/darkraise-ui/dist/components/table/Table.d.ts:11`. `Button` takes `asChild`: `web/node_modules/darkraise-ui/dist/components/button/Button.d.ts:7`.
- `UnsavedGuard` keeps the page when `onSave` resolves `false` (`onSave().then((ok) => (ok ? setSaved(true) : reset()))`): `web/src/components/unsaved-guard.tsx`.
- `ConfirmDialog` calls `run` on its action, then `onClose` as the dialog closes; Cancel calls only `onClose`: `web/src/components/confirm-dialog.tsx:31-44`.
- `queryClient.invalidateQueries` resolves once the active queries have refetched and does not throw on a failed refetch: https://tanstack.com/query/latest/docs/reference/QueryClient#queryclientinvalidatequeries
- The daemon refuses a global cleanup age under 24h, so the form does not check it: `internal/config/ghstorage.go:111`.
- Fixtures: `status.now` is `2026-10-03T14:05:00Z`, `config.github_storage` is `{ cleanup_max_age: "2d", repos: { darkmem: { cleanup: "6h" } } }`, and `github-storage.json` lists darkmem (209715200 artifact bytes, 2 artifacts, 71431401 cache bytes, 1 cache, 90 days), darkcloud (20971520, 1 artifact, 7 days) and sandbox (empty but with an error); the quota is level `warn`, used 260.5 of 363.28125 GB-h, estimate 262.9, projection 396.95, resetting `2026-11-01T00:00:00Z` (read 2026-10-13 from `web/src/api/fixtures/`).
- `githubStorageBusy` is true while `operation` has no `finished_at`: `web/src/api/hooks.ts:246`.
- `StatCard` renders `<section aria-label={label}>`, so Task 4's tests find a tile as a region by its label: `web/src/components/stat-card.tsx:10`.
- "No repository matches." also shows when the filter is empty, every repository is empty and the switch is off; that is intended (spec §2.4, "with rows to show but none shown"), and the switch beside it says how to show them.
- The Execution line is inline, so the session implements every task itself; the `**Implementer:**` lines are what dr-superpowers:executing-plans reads only for a task it delegates, and no task here is heavy enough to be delegated.
- Task 5's tooling: Vite proxies to `GHR_DEV_URL` (`web/vite.config.ts:6`); the daemon socket serves `/status`, `/config` and the other paths without an `/api` prefix (`internal/api/server.go:197`); Chrome is at `/opt/google/chrome/chrome`, `--blink-settings=preferredColorScheme=0` gives dark mode, and Node has a global `WebSocket` (all used for this repository's screenshots on 2026-10-13, this session and the one recorded in the `ghr-local-ui-screenshots` memory).

## Task index

1. GitHub storage section in Settings
2. Repository table: filter, empty switch, sort, totals, retention line
3. Artifacts page: drop the Policy card, sum up the policy
4. Quota stat tiles
5. Visual check

---

### Task 1: GitHub storage section in Settings

**Files:**
- Create: `web/src/components/cleanup-preview.tsx`
- Create: `web/src/lib/storage-policy-form.ts`
- Create: `web/src/components/github-storage-section.tsx`
- Create: `web/src/components/github-storage-section.test.tsx`
- Modify: `web/src/components/storage-policy-card.tsx` (CleanupPreview moves out)
- Modify: `web/src/components/artifact-repo-table.tsx:14` (import path)
- Modify: `web/src/pages/settings.tsx`
- Test: `web/src/pages/settings.test.tsx`

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces: Contracts `CleanupPreview` and the `github-storage` section.

**Items:** 2

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 2 - spec 0 - coupling 1 - risk 1 = 4

- [ ] **Step 1: Write the failing section test**

Create `web/src/components/github-storage-section.test.tsx`:

```tsx
import { screen, waitFor, within } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import { type Call, json, mockApi, noContent } from "@/test/api"
import { authedRoutes } from "@/test/fixtures"
import { renderApp } from "@/test/render"

async function draw(routes: Record<string, unknown> = {}) {
  const { calls } = mockApi(authedRoutes({ "PATCH /api/config": () => noContent(), ...routes }))
  const view = renderApp("/settings/github-storage")
  await screen.findByRole("region", { name: "Policy" })
  return { ...view, calls }
}

const field = (name: string) => screen.getByRole("spinbutton", { name }) as HTMLInputElement
const age = () => screen.getByRole("textbox", { name: "Cleanup age" }) as HTMLInputElement
const patches = (calls: Call[]) => calls.filter((c) => c.method === "PATCH")

describe("GitHub storage section", () => {
  it("shows the saved policy with the defaults filled in", async () => {
    await draw()
    expect(field("Included storage").value).toBe("500")
    expect(field("Warning at").value).toBe("70")
    expect(field("Critical at").value).toBe("90")
    expect(age().value).toBe("2d")
    expect(screen.queryByText(/unsaved change/)).toBeNull()
    expect(screen.getByRole("link", { name: "Artifacts page" })).toHaveAttribute("href", "/artifacts")
  })

  it("previews a new cleanup age and saves it once confirmed", async () => {
    const { user, calls } = await draw({
      "GET /api/github-storage/preview": [
        { repo: "darkcloud", count: 1, bytes: 20971520 },
        { repo: "darkmem", count: 2, bytes: 209715200 },
      ],
    })
    await user.clear(age())
    await user.type(age(), "1d")
    expect(screen.getByText("1 unsaved change")).toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: "Save changes" }))
    const dialog = await screen.findByRole("alertdialog", { name: "Clean artifacts after 1d?" })
    expect(dialog).toHaveTextContent("Cleanup does not protect an artifact that a different run downloads later")
    expect(dialog).toHaveTextContent("Up to 3 artifacts (230.7 MB) older than 1d would go at the next check.")
    expect(within(dialog).getByText("darkmem: up to 2 artifacts, 209.7 MB")).toBeInTheDocument()
    expect(calls.find((c) => c.path === "/api/github-storage/preview")?.search).toBe("?cleanup=1d")
    expect(patches(calls)).toHaveLength(0)
    await user.click(within(dialog).getByRole("button", { name: "Save" }))
    await waitFor(() => expect(patches(calls).map((c) => c.body)).toEqual([{ github_storage: { cleanup_max_age: "1d" } }]))
    expect((await screen.findAllByText("Storage policy saved")).length).toBeGreaterThan(0)
  })

  it("keeps the edit and sends nothing when the preview is cancelled", async () => {
    const { user, calls } = await draw({ "GET /api/github-storage/preview": [] })
    await user.clear(age())
    await user.type(age(), "12h")
    await user.click(screen.getByRole("button", { name: "Save changes" }))
    const dialog = await screen.findByRole("alertdialog", { name: "Clean artifacts after 12h?" })
    expect(dialog).toHaveTextContent("No artifact is older than 12h now.")
    await user.click(within(dialog).getByRole("button", { name: "Cancel" }))
    await waitFor(() => expect(screen.queryByRole("alertdialog")).toBeNull())
    expect(patches(calls)).toHaveLength(0)
    expect(age().value).toBe("12h")
    expect(screen.getByText("1 unsaved change")).toBeInTheDocument()
  })

  it.each<[string, (u: Awaited<ReturnType<typeof draw>>["user"]) => Promise<void>, unknown]>([
    ["turns cleanup off without a preview", async (user) => user.clear(age()), { cleanup_max_age: "" }],
    [
      "saves the allowance and warning levels",
      async (user) => {
        await user.clear(field("Included storage"))
        await user.type(field("Included storage"), "1024")
        await user.clear(field("Warning at"))
        await user.type(field("Warning at"), "60")
        await user.clear(field("Critical at"))
        await user.type(field("Critical at"), "85")
      },
      { included_mb: 1024, warn_pct: [60, 85] },
    ],
  ])("%s", async (_name, edit, patch) => {
    const { user, calls } = await draw()
    await edit(user)
    await user.click(screen.getByRole("button", { name: "Save changes" }))
    await waitFor(() => expect(patches(calls).map((c) => c.body)).toEqual([{ github_storage: patch }]))
    expect(calls.some((c) => c.path === "/api/github-storage/preview")).toBe(false)
  })

  it.each<[string, string, string]>([
    ["Included storage", "0", "Enter a whole number of MB, at least 1"],
    ["Warning at", "95", "Enter two whole percentages from 1 to 100, the first lower"],
  ])("refuses %s %s before sending", async (name, value, error) => {
    const { user, calls } = await draw()
    await user.clear(field(name))
    await user.type(field(name), value)
    expect(await screen.findByText(error)).toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: "Save changes" }))
    expect((await screen.findAllByText("Fix the highlighted settings first")).length).toBeGreaterThan(0)
    expect(patches(calls)).toHaveLength(0)
  })

  it("shows the daemon's refusal and keeps the edit", async () => {
    const msg = "github_storage.cleanup_max_age must be at least 24h"
    const { user } = await draw({ "GET /api/github-storage/preview": [], "PATCH /api/config": () => json({ error: msg }, 400) })
    await user.clear(age())
    await user.type(age(), "12h")
    await user.click(screen.getByRole("button", { name: "Save changes" }))
    const dialog = await screen.findByRole("alertdialog", { name: "Clean artifacts after 12h?" })
    await user.click(within(dialog).getByRole("button", { name: "Save" }))
    expect((await screen.findAllByText(msg)).length).toBeGreaterThan(0)
    expect(age().value).toBe("12h")
  })

  it("asks before leaving with unsaved changes", async () => {
    const { user, router } = await draw()
    await user.clear(field("Included storage"))
    await user.type(field("Included storage"), "1024")
    await user.click(screen.getAllByRole("link", { name: "Dashboard" })[0] as HTMLElement)
    expect(await screen.findByText("You have 1 unsaved change on the GitHub storage page.")).toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: "Stay" }))
    expect(router.state.location.pathname).toBe("/settings/github-storage")
  })
})
```

- [ ] **Step 2: Update the Settings page test**

In `web/src/pages/settings.test.tsx`, in "opens on General with the section list beside it", replace the expected list with:

```tsx
    expect(within(nav).getAllByRole("link").map((a) => a.textContent)).toEqual([
      "General",
      "GitHub token",
      "GitHub storage",
      "Runner software",
      "Account",
      "Toolchains",
      "Storage",
    ])
```

and in the `it.each` of "shows the %s section at its own address", add a row after `["token", "GitHub token"],`:

```tsx
    ["github-storage", "GitHub storage"],
```

- [ ] **Step 3: Run both tests to verify they fail**

Run: `TMPDIR=/root/.cache/ghr-plan-tmp npx vitest run src/components/github-storage-section.test.tsx src/pages/settings.test.tsx`
Expected: FAIL — the section tests time out finding the "Policy" region (the page says "Settings has no such section."), and the nav list lacks "GitHub storage".

- [ ] **Step 4: Move CleanupPreview to its own file**

Create `web/src/components/cleanup-preview.tsx`:

```tsx
import type { StoragePreview } from "@/api/types"
import { humanBytes, plural } from "@/lib/format"

// What a cleanup age would delete now, for the confirmation it gets before it
// is saved, globally or for one repository.
export function CleanupPreview({ age, previews }: { age: string; previews: StoragePreview[] }) {
  const count = previews.reduce((n, p) => n + p.count, 0)
  const bytes = previews.reduce((n, p) => n + p.bytes, 0)
  if (count === 0) return <p className="text-sm">{`No artifact is older than ${age} now.`}</p>
  return (
    <div className="flex flex-col gap-1 text-sm">
      <p>{`Up to ${plural(count, "artifact")} (${humanBytes(bytes)}) older than ${age} would go at the next check.`}</p>
      <ul className="flex flex-col gap-0.5">
        {previews.map((p) => (
          <li key={p.repo}>{`${p.repo}: up to ${plural(p.count, "artifact")}, ${humanBytes(p.bytes)}`}</li>
        ))}
      </ul>
    </div>
  )
}
```

In `web/src/components/storage-policy-card.tsx`: delete the comment and the whole `export function CleanupPreview(...) { ... }` block (from `// What a cleanup age would delete now` to its closing `}`), delete the line `import { humanBytes, plural } from "@/lib/format"`, and add `import { CleanupPreview } from "@/components/cleanup-preview"` after the `ConfirmDialog` import.

In `web/src/components/artifact-repo-table.tsx`, replace `import { CleanupPreview } from "@/components/storage-policy-card"` with `import { CleanupPreview } from "@/components/cleanup-preview"`.

- [ ] **Step 5: Write the form hook**

Create `web/src/lib/storage-policy-form.ts`:

```ts
import { useQueryClient } from "@tanstack/react-query"
import { toast } from "darkraise-ui/components/sonner"
import { useState } from "react"
import { ApiError, api } from "@/api/client"
import { keys, useConfig, useStatus } from "@/api/hooks"
import type { Config, GitHubStoragePatch, StoragePreview } from "@/api/types"
import { changedKeys, settle, type Equal } from "@/lib/draft"
import { errorText } from "@/query"

export type PolicyKey = "included" | "warn" | "critical" | "age"
export type PolicyValues = Record<PolicyKey, string>
type Draft = Partial<PolicyValues>

// A changed, non-empty cleanup age waits here until its preview is confirmed.
export interface PendingCleanup {
  age: string
  previews: StoragePreview[]
  sent: Draft
  patch: GitHubStoragePatch
}

function loadedValues(c: Config): PolicyValues {
  const g = c.github_storage
  const [warn, critical] = g?.warn_pct ?? [70, 90]
  return { included: String(g?.included_mb ?? 500), warn: String(warn), critical: String(critical), age: g?.cleanup_max_age ?? "" }
}

const equal: Equal = (_key, a, b) => String(a).trim() === String(b).trim()

const whole = (s: string) => (/^\d+$/.test(s.trim()) ? Number(s.trim()) : Number.NaN)

function policyErrors(v: PolicyValues): Partial<Record<PolicyKey, string>> {
  const out: Partial<Record<PolicyKey, string>> = {}
  if (!(whole(v.included) >= 1)) out.included = "Enter a whole number of MB, at least 1"
  const w = whole(v.warn)
  const c = whole(v.critical)
  if (!(w >= 1 && c <= 100 && w < c)) out.critical = "Enter two whole percentages from 1 to 100, the first lower"
  return out
}

function toPatch(changed: PolicyKey[], v: PolicyValues): GitHubStoragePatch {
  const patch: GitHubStoragePatch = {}
  if (changed.includes("included")) patch.included_mb = whole(v.included)
  if (changed.includes("warn") || changed.includes("critical")) patch.warn_pct = [whole(v.warn), whole(v.critical)]
  if (changed.includes("age")) patch.cleanup_max_age = v.age.trim()
  return patch
}

export function useStoragePolicyForm() {
  const config = useConfig()
  const status = useStatus()
  const queryClient = useQueryClient()
  const [draft, setDraft] = useState<Draft>({})
  const [saving, setSaving] = useState(false)
  const [rejection, setRejection] = useState("")
  const [pending, setPending] = useState<PendingCleanup | null>(null)

  const loaded = config.data ? loadedValues(config.data) : undefined
  const values: PolicyValues = { included: "", warn: "", critical: "", age: "", ...loaded, ...draft }
  const changed = loaded ? (changedKeys(draft, loaded, equal) as PolicyKey[]) : []
  const errors = loaded ? policyErrors(values) : {}

  const set = (key: PolicyKey) => (value: string) => setDraft((d) => ({ ...d, [key]: value }))

  function discard() {
    setDraft({})
    setRejection("")
  }

  async function send(sent: Draft, patch: GitHubStoragePatch): Promise<boolean> {
    setSaving(true)
    setRejection("")
    try {
      await api.patchConfig({ github_storage: patch })
    } catch (err) {
      setSaving(false)
      if (err instanceof ApiError && err.status >= 400 && err.status < 500) {
        setRejection(err.message)
        toast.error("Storage policy not saved")
      } else {
        toast.error(`Storage policy not saved: ${errorText(err)}`)
      }
      return false
    }
    toast.success("Storage policy saved")
    // Settled only after the config is read back, so the fields never flash
    // the values from before the save.
    await queryClient.invalidateQueries({ queryKey: keys.config })
    void queryClient.invalidateQueries({ queryKey: keys.githubStorage })
    setDraft((d) => settle(d, sent, equal) as Draft)
    setSaving(false)
    return true
  }

  async function save(): Promise<boolean> {
    if (changed.length === 0) return true
    if (saving || pending) return false
    if (Object.keys(errors).length > 0) {
      toast.error("Fix the highlighted settings first")
      return false
    }
    const sent: Draft = Object.fromEntries(changed.map((k) => [k, values[k]]))
    const patch = toPatch(changed, values)
    const age = patch.cleanup_max_age
    if (age === undefined || age === "") return send(sent, patch)
    setRejection("")
    try {
      setPending({ age, previews: await api.storagePreview(age), sent, patch })
    } catch (err) {
      setRejection(errorText(err))
      toast.error("Storage policy not saved")
    }
    // The preview's confirmation sends the save, so a leave prompt that asked
    // for this one keeps the page.
    return false
  }

  function confirmPending() {
    if (!pending) return
    const p = pending
    setPending(null)
    void send(p.sent, p.patch)
  }

  return {
    config: config.data,
    configError: config.isError ? errorText(config.error) : "",
    retryConfig: () => void config.refetch(),
    values,
    changed,
    errors,
    offline: status.isError,
    saving,
    rejection,
    pending,
    set,
    save,
    discard,
    confirmPending,
    cancelPending: () => setPending(null),
  }
}
```

- [ ] **Step 6: Write the section**

Create `web/src/components/github-storage-section.tsx`:

```tsx
import { Link } from "@tanstack/react-router"
import { Input } from "darkraise-ui/components/input"
import { Spinner } from "darkraise-ui/components/spinner"
import { PageHeader } from "darkraise-ui/layout"
import { CleanupPreview } from "@/components/cleanup-preview"
import { ConfirmDialog, type Confirm } from "@/components/confirm-dialog"
import { ErrorLine } from "@/components/page/error-line"
import { Field, FieldGroup } from "@/components/page/field"
import { SaveBar } from "@/components/save-bar"
import { UnsavedGuard } from "@/components/unsaved-guard"
import { FIELD_WIDTH } from "@/lib/field-width"
import { CLEANUP_WARNING } from "@/lib/quota"
import { type PolicyKey, useStoragePolicyForm } from "@/lib/storage-policy-form"

export function GitHubStorageSection() {
  const form = useStoragePolicyForm()
  const { values, errors, offline, pending } = form
  const changed = (...keys: PolicyKey[]) => keys.some((k) => form.changed.includes(k))
  const confirm: Confirm | null = pending && {
    title: `Clean artifacts after ${pending.age}?`,
    body: CLEANUP_WARNING,
    details: <CleanupPreview age={pending.age} previews={pending.previews} />,
    action: "Save",
    destructive: pending.previews.some((p) => p.count > 0),
    run: form.confirmPending,
  }
  return (
    <>
      <PageHeader title="GitHub storage" description="Saved in config.yaml as github_storage." />
      {!form.config ? (
        form.configError ? (
          <ErrorLine onRetry={form.retryConfig}>{form.configError}</ErrorLine>
        ) : (
          <Spinner label="Loading" />
        )
      ) : (
        <>
          <div className="flex min-w-0 flex-col gap-4">
            {form.rejection && <ErrorLine>{form.rejection}</ErrorLine>}
            <FieldGroup id="storage-policy" title="Policy">
              <Field label="Included storage" htmlFor="included_mb" help="The plan's allowance for artifacts and Packages, in MB." error={errors.included} changed={changed("included")}>
                <Input id="included_mb" type="number" min={1} suffix="MB" groupClassName={FIELD_WIDTH} className="min-w-0" value={values.included} disabled={offline} onChange={(e) => form.set("included")(e.target.value)} />
              </Field>
              <Field label="Warning levels" help="Warn, then warn louder, when the estimate reaches these shares of the allowance." error={errors.critical} changed={changed("warn", "critical")}>
                <Input aria-label="Warning at" type="number" min={1} max={100} suffix="%" groupClassName="w-28" className="min-w-0" value={values.warn} disabled={offline} onChange={(e) => form.set("warn")(e.target.value)} />
                <Input aria-label="Critical at" type="number" min={1} max={100} suffix="%" groupClassName="w-28" className="min-w-0" value={values.critical} disabled={offline} onChange={(e) => form.set("critical")(e.target.value)} />
              </Field>
              <Field label="Cleanup age" htmlFor="cleanup_max_age" help={`Delete artifacts older than this, such as 2d; empty is off, and at least 24h. ${CLEANUP_WARNING}`} changed={changed("age")}>
                <Input id="cleanup_max_age" className={`${FIELD_WIDTH} font-mono`} placeholder="off" value={values.age} disabled={offline} onChange={(e) => form.set("age")(e.target.value)} />
              </Field>
            </FieldGroup>
            <p className="text-sm text-muted-foreground">
              A repository's own cleanup age and retention are set in its row on the{" "}
              <Link to="/artifacts" className="text-primary hover:underline">
                Artifacts page
              </Link>
              .
            </p>
          </div>
          <SaveBar count={form.changed.length} saving={form.saving} disabled={offline} onSave={() => void form.save()} onDiscard={form.discard} />
          <UnsavedGuard count={form.changed.length} page="GitHub storage" saving={form.saving} onSave={form.save} onDiscard={form.discard} />
          <ConfirmDialog confirm={confirm} onClose={form.cancelPending} />
        </>
      )}
    </>
  )
}
```

- [ ] **Step 7: Add the section to Settings**

In `web/src/pages/settings.tsx`:

- Add `import { GitHubStorageSection } from "@/components/github-storage-section"` after the `ErrorLine` import.
- Replace the `CONFIG` constant with:

```tsx
const CONFIG = [
  { id: "", title: "General" },
  { id: "token", title: "GitHub token" },
  { id: "github-storage", title: "GitHub storage" },
  { id: "runner", title: "Runner software" },
  { id: "account", title: "Account" },
] as const
```

- In `SectionContent`, add after the `case "token":` block:

```tsx
    case "github-storage":
      return <GitHubStorageSection />
```

- [ ] **Step 8: Run the tests to verify they pass**

Run: `TMPDIR=/root/.cache/ghr-plan-tmp npx vitest run src/components/github-storage-section.test.tsx src/pages/settings.test.tsx src/components/storage-policy-card.test.tsx src/components/artifact-repo-table.test.tsx`
Expected: PASS, every file.

- [ ] **Step 9: Type check and lint**

Run: `TMPDIR=/root/.cache/ghr-plan-tmp npm run typecheck && TMPDIR=/root/.cache/ghr-plan-tmp npm run lint`
Expected: both exit 0 with no warnings.

- [ ] **Step 10: Commit**

```bash
git add web/src/components/cleanup-preview.tsx web/src/lib/storage-policy-form.ts web/src/components/github-storage-section.tsx web/src/components/github-storage-section.test.tsx web/src/components/storage-policy-card.tsx web/src/components/artifact-repo-table.tsx web/src/pages/settings.tsx web/src/pages/settings.test.tsx
git commit -m "feat(web): add GitHub storage settings section"
```

### Task 2: Repository table: filter, empty switch, sort, totals, retention line

**Files:**
- Create: `web/src/components/retention-all.tsx`
- Modify: `web/src/lib/format.ts` (add `repositories`)
- Modify: `web/src/components/artifact-repo-table.tsx`
- Test: `web/src/components/artifact-repo-table.test.tsx`

**Interfaces:**
- Consumes: Contracts `CleanupPreview` (Task 1), through the import block in Step 5.
- Produces: nothing later tasks use.

**Items:** 1

**Implementer:** dr-superpowers:impl-sonnet-low
**Evaluation:** files 1 - spec 0 - coupling 0 - risk 1 = 2

Until Task 3 deletes it, the old Policy card still shows its own "Retention for all" on the Artifacts page; that duplicate is expected between the two tasks.

- [ ] **Step 1: Write the failing tests**

In `web/src/components/artifact-repo-table.test.tsx`:

Change the type import to `import type { GitHubStorage, RepoStorage } from "@/api/types"`, and add below the `row` helper:

```tsx
function repo(name: string, over: Partial<RepoStorage> = {}): RepoStorage {
  return {
    name,
    visibility: "private",
    covered: true,
    artifact_count: 0,
    artifact_bytes: 0,
    cache_count: 0,
    cache_bytes: 0,
    retention_days: 7,
    retention_max_days: 400,
    cleanup: "off",
    cleanup_source: "none",
    artifacts: [],
    caches: [],
    ...over,
  }
}

// The body rows' repository names; the first row is the header, the last the totals.
const listed = () =>
  screen
    .getAllByRole("row")
    .slice(1, -1)
    .map((r) => within(r).getByRole("rowheader").textContent?.trim().split(" ")[0])
```

In "lists each repository's artifacts, caches, retention and cleanup", change `const rows = screen.getAllByRole("row").slice(1)` to `const rows = screen.getAllByRole("row").slice(1, -1)`.

Replace the whole `it.each<[string, string, boolean]>([...])("at level %j, hints %s to lower its 90-day retention: %s", ...)` test with these tests:

```tsx
  it.each<[string, string | null]>([
    ["warn", "1 repository keeps GitHub's 90-day default; lowering retention saves storage."],
    ["", null],
  ])("at level %j, the retention line reads %j", async (level, line) => {
    mockApi({})
    const quota = fixtures.githubStorage.quota && { ...fixtures.githubStorage.quota, level }
    await draw({ ...fixtures.githubStorage, quota })
    expect(screen.queryByText(/90-day default/)?.textContent ?? null).toBe(line)
  })

  it("hides empty repositories behind a switch, largest artifacts first, then caches, then name", async () => {
    mockApi({})
    const { user } = await draw({
      ...fixtures.githubStorage,
      repos: [
        repo("Beta"),
        repo("alpha"),
        repo("beta", { cache_count: 1, cache_bytes: 500 }),
        repo("gamma", { artifact_count: 1, artifact_bytes: 10 }),
        repo("delta", { cache_count: 2, cache_bytes: 900 }),
        repo("epsilon", { artifact_count: 1, artifact_bytes: 10, cache_count: 1, cache_bytes: 5 }),
      ],
    })
    expect(listed()).toEqual(["epsilon", "gamma", "delta", "beta"])
    await user.click(screen.getByRole("switch", { name: "Show 2 empty repositories" }))
    expect(listed()).toEqual(["epsilon", "gamma", "delta", "beta", "alpha", "Beta"])
  })

  it("filters by name ignoring case, showing a matching empty repository", async () => {
    mockApi({})
    const { user } = await draw({
      ...fixtures.githubStorage,
      repos: [repo("Alpha"), repo("alpine", { artifact_count: 1, artifact_bytes: 10 }), repo("beta", { artifact_count: 1, artifact_bytes: 20 })],
    })
    const filter = screen.getByRole("textbox", { name: "Filter repositories" })
    await user.type(filter, "ALP")
    expect(listed()).toEqual(["alpine", "Alpha"])
    await user.clear(filter)
    await user.type(filter, "nope")
    expect(screen.getByText("No repository matches.")).toBeInTheDocument()
  })

  it("totals every repository whatever the filter shows", async () => {
    mockApi({})
    const { user } = await draw()
    await user.type(screen.getByRole("textbox", { name: "Filter repositories" }), "darkcloud")
    const total = screen.getByRole("row", { name: /^All 3 repositories/ })
    for (const text of ["230.7 MB", "3 artifacts", "71.4 MB", "1 cache"]) {
      expect(within(total).getByText(text)).toBeInTheDocument()
    }
  })

  it("sets retention on every repository after a warning", async () => {
    const { calls } = mockApi({ "POST /api/github-storage/retention": () => json({ id: "op-2" }, 202) })
    const { user } = await draw()
    await user.type(screen.getByRole("spinbutton", { name: "Retention for all" }), "7")
    await user.click(screen.getByRole("button", { name: "Set retention" }))
    const dialog = await screen.findByRole("alertdialog", { name: "Set retention to 7 days on 3 repositories?" })
    expect(calls.some((c) => c.method === "POST")).toBe(false)
    expect(dialog).toHaveTextContent(
      "GitHub's retention also limits how long it keeps runs and logs, which ghr's Actions and log pages read. A repository whose maximum is lower is skipped.",
    )
    await user.click(within(dialog).getByRole("button", { name: "Set retention" }))
    await waitFor(() => expect(calls.find((c) => c.method === "POST")?.body).toEqual({ days: 7 }))
    expect((await screen.findAllByText("Retention change started")).length).toBeGreaterThan(0)
  })

  it("holds Set retention while an operation runs", async () => {
    mockApi({})
    const operation = fixtures.githubStorage.operation && { ...fixtures.githubStorage.operation, finished_at: undefined }
    const { user } = await draw({ ...fixtures.githubStorage, operation })
    await user.type(screen.getByRole("spinbutton", { name: "Retention for all" }), "7")
    expect(screen.getByRole("button", { name: "Set retention" })).toBeDisabled()
  })
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `TMPDIR=/root/.cache/ghr-plan-tmp npx vitest run src/components/artifact-repo-table.test.tsx`
Expected: FAIL — no "Filter repositories" textbox, no switch, no "Retention for all", no totals row, and the old per-row hint text.

- [ ] **Step 3: Add the repositories helper**

Append to `web/src/lib/format.ts`:

```ts
export function repositories(n: number): string {
  return n === 1 ? "1 repository" : `${n} repositories`
}
```

- [ ] **Step 4: Write RetentionAll**

Create `web/src/components/retention-all.tsx`:

```tsx
import { useMutation, useQueryClient } from "@tanstack/react-query"
import { Button } from "darkraise-ui/components/button"
import { Input } from "darkraise-ui/components/input"
import { Label } from "darkraise-ui/components/label"
import { toast } from "darkraise-ui/components/sonner"
import { useState } from "react"
import { api } from "@/api/client"
import { keys } from "@/api/hooks"
import type { Confirm } from "@/components/confirm-dialog"
import { repositories } from "@/lib/format"
import { RETENTION_WARNING } from "@/lib/quota"

export function RetentionAll({ count, offline, busy, ask }: { count: number; offline: boolean; busy: boolean; ask: (c: Confirm) => void }) {
  const queryClient = useQueryClient()
  const [days, setDays] = useState("")
  const retention = useMutation({
    mutationFn: (n: number) => api.setRetentionAll(n),
    onSuccess: () => {
      toast.success("Retention change started")
      setDays("")
    },
    onSettled: () => queryClient.invalidateQueries({ queryKey: keys.githubStorage }),
  })
  const n = /^\d+$/.test(days.trim()) ? Number(days.trim()) : Number.NaN
  const ok = n >= 1 && n <= 400
  return (
    <div className="flex flex-col gap-1">
      <div className="flex flex-wrap items-center gap-2">
        <Label htmlFor="retention_all">Retention for all</Label>
        <Input id="retention_all" type="number" min={1} max={400} suffix="days" groupClassName="w-36" className="min-w-0" value={days} disabled={offline} onChange={(e) => setDays(e.target.value)} />
        <Button
          variant="outline"
          disabled={offline || busy || !ok || count === 0 || retention.isPending}
          onClick={() =>
            ask({
              title: `Set retention to ${n} days on ${repositories(count)}?`,
              body: `${RETENTION_WARNING} A repository whose maximum is lower is skipped.`,
              action: "Set retention",
              run: () => retention.mutate(n),
            })
          }
        >
          Set retention
        </Button>
      </div>
      {days !== "" && !ok && <p className="text-xs text-destructive">Enter a whole number of days from 1 to 400</p>}
    </div>
  )
}
```

- [ ] **Step 5: Rework the table**

In `web/src/components/artifact-repo-table.tsx`:

Replace the file's import block (everything above `const NUM`) with:

```tsx
import { useMutation, useQueryClient } from "@tanstack/react-query"
import { Button } from "darkraise-ui/components/button"
import { Input } from "darkraise-ui/components/input"
import { Label } from "darkraise-ui/components/label"
import { toast } from "darkraise-ui/components/sonner"
import { Switch } from "darkraise-ui/components/switch"
import { Table, TableBody, TableCell, TableFooter, TableHead, TableHeader, TableRow } from "darkraise-ui/components/table"
import { ChevronRight, Pencil } from "lucide-react"
import { Fragment, useState } from "react"
import { api } from "@/api/client"
import { githubStorageBusy, keys } from "@/api/hooks"
import type { Config, GitHubStorage, RepoStorage } from "@/api/types"
import { ArtifactRepoDetail } from "@/components/artifact-repo-detail"
import { CleanupPreview } from "@/components/cleanup-preview"
import { ConfirmDialog, type Confirm } from "@/components/confirm-dialog"
import { Section } from "@/components/page/section"
import { RetentionAll } from "@/components/retention-all"
import { humanBytes, plural, repositories } from "@/lib/format"
import { CLEANUP_WARNING, RETENTION_WARNING } from "@/lib/quota"
import { errorText } from "@/query"
```

Add above `export function ArtifactRepoTable`:

```tsx
const isEmpty = (r: RepoStorage) => r.artifact_count === 0 && r.cache_count === 0 && !r.error

const bySize = (a: RepoStorage, b: RepoStorage) =>
  b.artifact_bytes - a.artifact_bytes || b.cache_bytes - a.cache_bytes || a.name.toLowerCase().localeCompare(b.name.toLowerCase())

// A filter with text shows every repository it matches, empty or not; without
// one, empty repositories wait for the switch.
function shownRepos(repos: RepoStorage[], filter: string, showEmpty: boolean): RepoStorage[] {
  const q = filter.trim().toLowerCase()
  return repos.filter((r) => (q ? r.name.toLowerCase().includes(q) : showEmpty || !isEmpty(r))).sort(bySize)
}
```

Replace the whole `export function ArtifactRepoTable(...) { ... }` with:

```tsx
export function ArtifactRepoTable({ storage, config, offline }: { storage: GitHubStorage; config: Config | undefined; offline: boolean }) {
  const [confirm, setConfirm] = useState<Confirm | null>(null)
  const [open, setOpen] = useState<string[]>([])
  const [filter, setFilter] = useState("")
  const [showEmpty, setShowEmpty] = useState(false)
  const busy = githubStorageBusy(storage)
  const globalAge = config?.github_storage?.cleanup_max_age ?? ""
  const warned = (storage.quota?.level ?? "") !== ""
  const repos = storage.repos
  const shown = shownRepos(repos, filter, showEmpty)
  const empty = repos.filter(isEmpty).length
  const atDefault = repos.filter((r) => r.retention_days === 90).length
  const sum = (pick: (r: RepoStorage) => number) => repos.reduce((n, r) => n + pick(r), 0)
  return (
    <Section id="repositories" title="Repositories" flush>
      {repos.length === 0 ? (
        <p className="p-4 text-sm text-muted-foreground">No repositories listed yet.</p>
      ) : (
        <>
          <div className="flex flex-col gap-3 border-b border-border p-4">
            <div className="flex flex-wrap items-center gap-x-4 gap-y-2">
              <Input aria-label="Filter repositories" placeholder="Filter repositories" className="w-64 max-w-full" value={filter} onChange={(e) => setFilter(e.target.value)} />
              {empty > 0 && (
                <span className="flex items-center gap-2">
                  <Switch id="show-empty" checked={showEmpty} onCheckedChange={setShowEmpty} />
                  <Label htmlFor="show-empty">{`Show ${empty} empty ${empty === 1 ? "repository" : "repositories"}`}</Label>
                </span>
              )}
            </div>
            {warned && atDefault > 0 && (
              <p className="text-sm text-warning">{`${repositories(atDefault)} ${atDefault === 1 ? "keeps" : "keep"} GitHub's 90-day default; lowering retention saves storage.`}</p>
            )}
            <RetentionAll count={repos.length} offline={offline} busy={busy} ask={setConfirm} />
          </div>
          <div className="overflow-x-auto [&_.dr-table-wrapper]:border-0">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead className="pl-4">Repository</TableHead>
                  <TableHead className="text-right">Artifacts</TableHead>
                  <TableHead className={`text-right ${WIDE}`}>Caches</TableHead>
                  <TableHead className="text-right">Retention</TableHead>
                  <TableHead className="pr-4">Cleanup</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {shown.length === 0 && (
                  <TableRow>
                    <TableCell colSpan={5} className="p-4 text-sm text-muted-foreground">
                      No repository matches.
                    </TableCell>
                  </TableRow>
                )}
                {shown.map((r) => {
                  const expanded = open.includes(r.name)
                  return (
                    <Fragment key={r.name}>
                      <TableRow>
                        <th scope="row" className="py-2 pl-4 text-left align-top font-normal">
                          <span className="flex items-center gap-2">
                            <button
                              type="button"
                              aria-expanded={expanded}
                              className="flex items-center gap-1 hover:text-primary"
                              onClick={() => setOpen((o) => (expanded ? o.filter((n) => n !== r.name) : [...o, r.name]))}
                            >
                              <ChevronRight size={14} aria-hidden="true" className={expanded ? "rotate-90" : ""} />
                              {r.name}
                            </button>{" "}
                            {r.visibility === "public" && <span className="text-xs text-muted-foreground">public</span>}
                          </span>
                          {r.error && <p className="text-xs whitespace-normal text-destructive">{r.error}</p>}
                        </th>
                        <TableCell className={`align-top ${NUM}`}>
                          <Sized bytes={r.artifact_bytes} count={r.artifact_count} word="artifact" />
                        </TableCell>
                        <TableCell className={`align-top ${NUM} ${WIDE}`}>
                          <Sized bytes={r.cache_bytes} count={r.cache_count} word="cache" />
                        </TableCell>
                        <TableCell className="text-right align-top">
                          <RetentionCell repo={r} offline={offline} ask={setConfirm} />
                        </TableCell>
                        <TableCell className="pr-4 align-top">
                          <CleanupCell repo={r} globalAge={globalAge} offline={offline || !config} ask={setConfirm} />
                        </TableCell>
                      </TableRow>
                      {expanded && (
                        <TableRow>
                          <TableCell colSpan={5} className="bg-background p-0 whitespace-normal">
                            <ArtifactRepoDetail repo={r} owner={config?.owner ?? ""} busy={busy} offline={offline} ask={setConfirm} />
                          </TableCell>
                        </TableRow>
                      )}
                    </Fragment>
                  )
                })}
              </TableBody>
              <TableFooter>
                <TableRow>
                  <th scope="row" className="py-2 pl-4 text-left align-top font-semibold">
                    {`All ${repositories(repos.length)}`}
                  </th>
                  <TableCell className={`align-top ${NUM}`}>
                    <Sized bytes={sum((r) => r.artifact_bytes)} count={sum((r) => r.artifact_count)} word="artifact" />
                  </TableCell>
                  <TableCell className={`align-top ${NUM} ${WIDE}`}>
                    <Sized bytes={sum((r) => r.cache_bytes)} count={sum((r) => r.cache_count)} word="cache" />
                  </TableCell>
                  <TableCell colSpan={2} />
                </TableRow>
              </TableFooter>
            </Table>
          </div>
        </>
      )}
      <ConfirmDialog confirm={confirm} onClose={() => setConfirm(null)} />
    </Section>
  )
}
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `TMPDIR=/root/.cache/ghr-plan-tmp npx vitest run src/components/artifact-repo-table.test.tsx src/components/artifact-repo-detail.test.tsx src/pages/artifacts.test.tsx`
Expected: PASS.

- [ ] **Step 7: Type check and lint**

Run: `TMPDIR=/root/.cache/ghr-plan-tmp npm run typecheck && TMPDIR=/root/.cache/ghr-plan-tmp npm run lint`
Expected: both exit 0 with no warnings.

- [ ] **Step 8: Commit**

```bash
git add web/src/components/retention-all.tsx web/src/lib/format.ts web/src/components/artifact-repo-table.tsx web/src/components/artifact-repo-table.test.tsx
git commit -m "feat(web): filter and total the artifact repositories"
```

### Task 3: Artifacts page: drop the Policy card, sum up the policy

**Files:**
- Modify: `web/src/pages/artifacts.tsx`
- Test: `web/src/pages/artifacts.test.tsx`
- Delete: `web/src/components/storage-policy-card.tsx`
- Delete: `web/src/components/storage-policy-card.test.tsx`
- Modify: `README.md:126,129`

**Interfaces:**
- Consumes: Contracts `github-storage` section (the link target) and `CleanupPreview` (no file may still import from `storage-policy-card.tsx`).
- Produces: nothing later tasks use.

**Items:** 1, 2

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 2 - spec 0 - coupling 1 - risk 1 = 4

- [ ] **Step 1: Write the failing tests**

In `web/src/pages/artifacts.test.tsx`, add below the imports:

```tsx
const SUMMARY = " · cleanup 2d (1 repository override) · warnings at 70% and 90%"
```

Replace both `"Checked 14:00"` with `` `Checked 14:00${SUMMARY}` `` and `"Not checked yet"` with `` `Not checked yet${SUMMARY}` ``. Then add inside the `describe`:

```tsx
  it("sums up a policy without overrides and links to it in Settings, without a policy form", async () => {
    mockApi(authedRoutes({ "GET /api/config": { ...fixtures.config, github_storage: { included_mb: 1024, warn_pct: [60, 85] } } }))
    renderApp("/artifacts")
    expect(await screen.findByText("Checked 14:00 · cleanup off · warnings at 60% and 85%")).toBeInTheDocument()
    expect(screen.getByRole("link", { name: "Policy" })).toHaveAttribute("href", "/settings/github-storage")
    expect(screen.queryByRole("region", { name: "Policy" })).toBeNull()
    expect(screen.queryByRole("spinbutton", { name: "Included storage" })).toBeNull()
  })
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `TMPDIR=/root/.cache/ghr-plan-tmp npx vitest run src/pages/artifacts.test.tsx`
Expected: FAIL — the description is still "Checked 14:00" and the Policy card is still on the page.

- [ ] **Step 3: Rework the page**

Replace `web/src/pages/artifacts.tsx` with:

```tsx
import { useMutation, useQueryClient } from "@tanstack/react-query"
import { Link } from "@tanstack/react-router"
import { Button } from "darkraise-ui/components/button"
import { toast } from "darkraise-ui/components/sonner"
import { Spinner } from "darkraise-ui/components/spinner"
import { PageHeader } from "darkraise-ui/layout"
import { api } from "@/api/client"
import { keys, useConfig, useGitHubStorage, useStatus } from "@/api/hooks"
import type { Config } from "@/api/types"
import { ArtifactRepoTable } from "@/components/artifact-repo-table"
import { ErrorLine } from "@/components/page/error-line"
import { QuotaCard } from "@/components/quota-card"
import { OperationProgress, RecentDeletions } from "@/components/storage-operations"
import { shortWhen } from "@/lib/format"
import { useNow } from "@/lib/use-now"
import { errorText } from "@/query"

function policySummary(config: Config | undefined): string {
  if (!config) return ""
  const g = config.github_storage
  const [warn, critical] = g?.warn_pct ?? [70, 90]
  const n = Object.keys(g?.repos ?? {}).length
  const overrides = n === 0 ? "" : ` (${n} repository override${n === 1 ? "" : "s"})`
  return ` · cleanup ${g?.cleanup_max_age || "off"}${overrides} · warnings at ${warn}% and ${critical}%`
}

export function ArtifactsPage() {
  const storage = useGitHubStorage()
  const status = useStatus()
  const config = useConfig()
  const queryClient = useQueryClient()
  const now = useNow()
  const refresh = useMutation({
    mutationFn: () => api.refreshGitHubStorage(),
    onSuccess: () => {
      toast.success("Check started")
    },
    onSettled: () => queryClient.invalidateQueries({ queryKey: keys.githubStorage }),
  })
  const data = storage.data
  const offline = status.isError
  const checked = data ? (data.checked_at ? `Checked ${shortWhen(data.checked_at, now)}` : "Not checked yet") : undefined
  return (
    <div className="flex flex-col gap-4">
      <PageHeader
        title="Artifacts"
        description={checked && `${checked}${policySummary(config.data)}`}
        actions={
          <>
            <Button variant="outline" asChild>
              <Link to="/settings/$section" params={{ section: "github-storage" }}>
                Policy
              </Link>
            </Button>
            <Button variant="outline" disabled={offline || refresh.isPending} onClick={() => refresh.mutate()}>
              Check now
            </Button>
          </>
        }
      />
      {storage.isError && <ErrorLine onRetry={() => void storage.refetch()}>{errorText(storage.error)}</ErrorLine>}
      {data?.error && <ErrorLine>{`Could not list the repositories: ${data.error}`}</ErrorLine>}
      {!data && !storage.isError && <Spinner label="Loading" />}
      {data && <QuotaCard storage={data} />}
      {data && <OperationProgress operation={data.operation} />}
      {data && <ArtifactRepoTable storage={data} config={config.data} offline={offline} />}
      {data && <RecentDeletions deletions={data.deletions} />}
    </div>
  )
}
```

- [ ] **Step 4: Delete the Policy card**

Run: `git rm web/src/components/storage-policy-card.tsx web/src/components/storage-policy-card.test.tsx`
Then: `rg -n "storage-policy-card" web/src` — Expected: no output.

- [ ] **Step 5: Update the README**

In `README.md`, section "GitHub artifact storage":

Replace `The **Artifacts** tab shows the month's use against the allowance, each repository's artifacts, caches and retention, and the cleanup policy.` with `The **Artifacts** tab shows the month's use against the allowance and each repository's artifacts, caches, retention and cleanup; the storage policy itself is set under **Settings**, **GitHub storage**.`

Replace `2. Set a cleanup age for the rest.` with `2. Set a cleanup age for the rest, under **Settings**, **GitHub storage**; a repository's own age is set in its row on the Artifacts page.`

- [ ] **Step 6: Run the tests to verify they pass**

Run: `TMPDIR=/root/.cache/ghr-plan-tmp npx vitest run src/pages/artifacts.test.tsx src/components/github-storage-section.test.tsx src/components/artifact-repo-table.test.tsx`
Expected: PASS.

- [ ] **Step 7: Type check and lint**

Run: `TMPDIR=/root/.cache/ghr-plan-tmp npm run typecheck && TMPDIR=/root/.cache/ghr-plan-tmp npm run lint`
Expected: both exit 0 with no warnings.

- [ ] **Step 8: Commit**

```bash
git add web/src/pages/artifacts.tsx web/src/pages/artifacts.test.tsx README.md
git commit -m "feat(web): move the storage policy off Artifacts"
```

### Task 4: Quota stat tiles

**Files:**
- Modify: `web/src/components/quota-card.tsx`
- Test: `web/src/components/quota-card.test.tsx`

**Interfaces:**
- Consumes: nothing from earlier tasks (uses the existing `StatCard` from `web/src/components/stat-card.tsx`).
- Produces: nothing later tasks use.

**Items:** 1

**Implementer:** dr-superpowers:impl-sonnet-low
**Evaluation:** files 1 - spec 0 - coupling 0 - risk 1 = 2

- [ ] **Step 1: Write the failing tests**

In `web/src/components/quota-card.test.tsx`, add below `draw`:

```tsx
const tile = (card: HTMLElement, name: string) => within(card).getByRole("region", { name })
```

In "shows the billed use against the allowance, with the estimate and projection marked", replace the two lines

```tsx
    expect(within(card).getByText("72%")).toBeInTheDocument()
    expect(within(card).getByText("261 GB-h of 363 GB-h used in October")).toBeInTheDocument()
```

with

```tsx
    expect(within(tile(card, "Used")).getByText("72%")).toBeInTheDocument()
    expect(within(tile(card, "Used")).getByText("261 GB-h of 363 GB-h")).toBeInTheDocument()
    expect(within(tile(card, "Estimated now")).getByText("263 GB-h")).toBeInTheDocument()
    expect(within(tile(card, "Projected by Nov 1")).getByText("397 GB-h")).toBeInTheDocument()
    expect(within(tile(card, "Resets")).getByText("Nov 1")).toBeInTheDocument()
    expect(within(tile(card, "Resets")).getByText("in 29 days")).toBeInTheDocument()
```

Add these tests inside the `describe`:

```tsx
  it.each<[string, string | null]>([
    ["exhausted", "text-destructive"],
    ["critical", "text-destructive"],
    ["warn", "text-warning"],
    ["projected", "text-warning"],
    ["", null],
  ])("tints the used share at level %j", (level, tint) => {
    const card = draw({ level })
    const value = within(tile(card, "Used")).getByText("72%")
    expect(value.className.split(" ").find((c) => c === "text-destructive" || c === "text-warning") ?? null).toBe(tint)
  })

  it("counts a reset tomorrow as one day", () => {
    const card = draw({ resets_at: "2026-10-04T00:00:00Z" })
    expect(within(tile(card, "Resets")).getByText("in 1 day")).toBeInTheDocument()
  })
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `TMPDIR=/root/.cache/ghr-plan-tmp npx vitest run src/components/quota-card.test.tsx`
Expected: FAIL — no "Used" region.

- [ ] **Step 3: Add the tiles**

In `web/src/components/quota-card.tsx`, add `import { StatCard } from "@/components/stat-card"` after the `Section` import, and add below the `TINT` constant:

```tsx
const VALUE_TINT: Record<string, string> = {
  exhausted: "text-destructive",
  critical: "text-destructive",
  warn: "text-warning",
  projected: "text-warning",
}

// The Dashboard's tile grid, with a rule below in place of its frame.
const TILES =
  "grid grid-cols-2 divide-border border-b border-border [&>*:nth-child(-n+2)]:border-b [&>*:nth-child(odd)]:border-r min-[641px]:grid-cols-4 min-[641px]:divide-x min-[641px]:[&>*:nth-child(-n+2)]:border-b-0"

// Calendar days in UTC; the reset is midnight UTC on a later day.
function untilReset(iso: string, now: number): string {
  const n = Math.floor(Date.parse(iso) / 86_400_000) - Math.floor(now / 86_400_000)
  return n === 1 ? "in 1 day" : `in ${n} days`
}
```

Replace the whole `export function QuotaCard(...) { ... }` with:

```tsx
export function QuotaCard({ storage }: { storage: GitHubStorage }) {
  const now = useNow()
  const q = storage.quota
  const aside = q && <span className="ml-auto font-mono text-sm text-muted-foreground">{`Billing read ${shortWhen(q.read_at, now)}`}</span>
  return (
    <Section id="quota" title="Quota" aside={aside} flush>
      {storage.quota_error && (
        <div className="border-b border-border p-4">
          <ErrorLine>{storage.quota_error}</ErrorLine>
        </div>
      )}
      {q && (
        <div className={TILES}>
          <StatCard
            label="Used"
            value={<span className={VALUE_TINT[q.level] ?? ""}>{`${Math.round((q.used_gb_hours / q.included_gb_hours) * 100)}%`}</span>}
            unit={`${gbh(q.used_gb_hours)} of ${gbh(q.included_gb_hours)}`}
          />
          <StatCard label="Estimated now" value={gbh(q.estimated_gb_hours)} />
          <StatCard label={`Projected by ${utcDay(q.resets_at)}`} value={gbh(q.projected_gb_hours)} />
          <StatCard label="Resets" value={utcDay(q.resets_at)} unit={untilReset(q.resets_at, now)} />
        </div>
      )}
      <div className="flex flex-col gap-3 p-4">
        {q ? (
          <>
            <QuotaMeter q={q} />
            <ul className="flex flex-col gap-0.5 text-sm">
              <li className="flex items-center gap-2">
                <span aria-hidden="true" className="h-3 w-0.5 bg-foreground" />
                {`Estimated now ${gbh(q.estimated_gb_hours)}`}
              </li>
              <li className="flex items-center gap-2">
                <span aria-hidden="true" className="h-3 w-0.5 bg-warning" />
                {`Projected by ${utcDay(q.resets_at)} ${gbh(q.projected_gb_hours)}`}
              </li>
            </ul>
            <p className="text-sm font-semibold">{outlook(q)}</p>
            <p className="text-sm">{`Actions ${gbh(q.actions_gb_hours)}, Packages ${gbh(q.packages_gb_hours)}`}</p>
          </>
        ) : (
          !storage.quota_error && <p className="text-sm text-muted-foreground">No billing read yet.</p>
        )}
        <p className="text-xs text-muted-foreground">
          GitHub recalculates storage every 6 to 12 hours, so the estimate adds 12 hours at the current rate. The month is taken to be the calendar month in UTC.
        </p>
      </div>
    </Section>
  )
}
```

`monthName` is no longer used: remove it from the `@/lib/quota` import.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `TMPDIR=/root/.cache/ghr-plan-tmp npx vitest run src/components/quota-card.test.tsx src/pages/artifacts.test.tsx`
Expected: PASS.

- [ ] **Step 5: Type check and lint**

Run: `TMPDIR=/root/.cache/ghr-plan-tmp npm run typecheck && TMPDIR=/root/.cache/ghr-plan-tmp npm run lint`
Expected: both exit 0 with no warnings.

- [ ] **Step 6: Commit**

```bash
git add web/src/components/quota-card.tsx web/src/components/quota-card.test.tsx
git commit -m "feat(web): show the quota as stat tiles"
```

### Task 5: Visual check

**Files:**
- Test: screenshots and a width check only; nothing in the repository changes.

**Interfaces:**
- Consumes: the finished pages from Tasks 1 to 4.
- Produces: nothing.

**Items:** 1

**Implementer:** dr-superpowers:impl-sonnet-low
**Evaluation:** files 0 - spec 1 - coupling 0 - risk 0 = 1

All scratch files go in the session's scratchpad directory (call it `$S`), not the repository. Every process started here is killed at the end.

- [ ] **Step 1: Run the full suites**

Run from `web/`: `TMPDIR=/root/.cache/ghr-plan-tmp npm test && TMPDIR=/root/.cache/ghr-plan-tmp npm run typecheck && TMPDIR=/root/.cache/ghr-plan-tmp npm run lint`
Then from `web/`: `TMPDIR=/root/.cache/ghr-plan-tmp npm run build` (the Go binary embeds `web/dist`, `web/embed.go:9`).
Run from the repository root: `TMPDIR=/root/.cache/ghr-plan-tmp GOTMPDIR=/root/.cache/ghr-plan-tmp PATH=/usr/local/go/bin:$PATH timeout 600 go test ./...`
Expected: every web test passes, both checks and the build exit 0, and every Go package prints `ok`.

- [ ] **Step 2: Snapshot live data and serve it**

Fetch read-only snapshots from the runner LXC (`root@192.168.0.99`, key SSH works) into `$S/live/`:

```bash
mkdir -p $S/live && for p in status config github-storage events setup storage; do ssh root@192.168.0.99 "curl -s --unix-socket /run/ghr/ghr.sock http://ghr/$p" > $S/live/$p.json; done
```

Write `$S/fixture_server.py`:

```python
import json, os, sys
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
LIVE, FIX = sys.argv[1], sys.argv[2]
class H(BaseHTTPRequestHandler):
    def log_message(self, *a): pass
    def send(self, code, body):
        b = json.dumps(body).encode() if not isinstance(body, bytes) else body
        self.send_response(code); self.send_header("Content-Type", "application/json"); self.send_header("Content-Length", str(len(b))); self.end_headers(); self.wfile.write(b)
    def do_GET(self):
        p = self.path.split("?")[0]
        if p == "/auth/state": return self.send(200, {"setup_required": False, "authenticated": True})
        if p.startswith("/api/changes"): return self.send(503, {"error": "no stream"})
        name = p[len("/api/"):].replace("/", "-") if p.startswith("/api/") else ""
        for d in (LIVE, FIX):
            f = os.path.join(d, name + ".json")
            if name and os.path.exists(f): return self.send(200, open(f, "rb").read())
        self.send(404, {"error": "no fixture for " + p})
    def do_POST(self): self.send(204, b"")
    do_PATCH = do_PUT = do_DELETE = do_POST
ThreadingHTTPServer(("127.0.0.1", 8099), H).serve_forever()
```

Start both as tracked background tasks: `python3 -I $S/fixture_server.py $S/live /root/repositories/ghr/web/src/api/fixtures`, and from `web/`: `TMPDIR=/root/.cache/ghr-plan-tmp GHR_DEV_URL=http://127.0.0.1:8099 npx vite --port 5173 --strictPort`.

- [ ] **Step 3: Desktop screenshots, light and dark**

For each of `http://localhost:5173/artifacts` and `http://localhost:5173/settings/github-storage`:

```bash
timeout 90 /opt/google/chrome/chrome --headless --no-sandbox --hide-scrollbars --user-data-dir=$S/chrome --window-size=1400,1800 --virtual-time-budget=20000 --screenshot=$S/<name>-light.png <url>
timeout 90 /opt/google/chrome/chrome --headless --no-sandbox --hide-scrollbars --user-data-dir=$S/chrome --window-size=1400,1800 --virtual-time-budget=20000 --blink-settings=preferredColorScheme=0 --screenshot=$S/<name>-dark.png <url>
```

Read each PNG. Expected on Artifacts: the header line "Checked … · cleanup … · warnings at …%" with Policy and Check now buttons; four quota tiles above the meter; the repository toolbar with the filter, "Show 56 empty repositories" (or the live count) and the Retention for all row; only non-empty repositories, largest artifacts first; the "All 64 repositories" totals row; no Policy card. Expected on Settings: "GitHub storage" selected in the side list and the three policy fields. Dark mode is readable in both. (A first run may come out blank while Vite compiles; run it again.)

- [ ] **Step 4: Width check at 390 px**

Headless Chrome windows are at least 500 px wide, so emulate the phone width over the DevTools protocol. Write `$S/narrow.mjs`:

```js
const [port, url] = process.argv.slice(2)
const tab = await (await fetch(`http://127.0.0.1:${port}/json/new?${url}`, { method: "PUT" })).json()
const ws = new WebSocket(tab.webSocketDebuggerUrl)
let id = 0
const waiting = new Map()
ws.onmessage = (e) => {
  const m = JSON.parse(e.data)
  if (waiting.has(m.id)) {
    waiting.get(m.id)(m.result)
    waiting.delete(m.id)
  }
}
const send = (method, params = {}) => new Promise((r) => { id += 1; waiting.set(id, r); ws.send(JSON.stringify({ id, method, params })) })
await new Promise((r) => (ws.onopen = r))
await send("Emulation.setDeviceMetricsOverride", { width: 390, height: 844, deviceScaleFactor: 1, mobile: true })
await send("Page.reload")
await new Promise((r) => setTimeout(r, 8000))
const out = await send("Runtime.evaluate", { expression: "JSON.stringify([document.documentElement.scrollWidth, document.documentElement.clientWidth])", returnByValue: true })
console.log(url, out.result.value)
ws.close()
```

Start Chrome as a tracked background task: `/opt/google/chrome/chrome --headless --no-sandbox --remote-debugging-port=9222 --user-data-dir=$S/chrome-cdp about:blank`. Then run `node $S/narrow.mjs 9222 http://localhost:5173/artifacts` and `node $S/narrow.mjs 9222 http://localhost:5173/settings/github-storage`.
Expected: both print two equal numbers (`[390,390]`): no horizontal page scroll.

- [ ] **Step 5: Stop everything started here**

Kill the Chrome, Vite and fixture server tasks by the PIDs of the tracked tasks (`pkill -P <pid>` then `kill <pid>`; find them with `pgrep -f "[f]ixture_server.py|[v]ite --port 5173|[r]emote-debugging-port=9222"`; never a bare `pkill -f` pattern, which matches the calling shell). Then run the same `pgrep` — Expected: no output.

- [ ] **Step 6: Report**

Post the screenshots' findings and the two width results. Any defect found goes back to the task that owns the file, as a fix commit there; deploying to the LXC waits for the owner's yes.
