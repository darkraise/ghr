import { screen, waitFor, within } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import type { LastPrune, Operation, Storage } from "@/api/types"
import { json, mockApi, noContent } from "@/test/api"
import { authedRoutes, fixtures } from "@/test/fixtures"
import { expectPill } from "@/test/pill"
import { renderApp } from "@/test/render"

const routes = (over: Record<string, unknown> = {}) => authedRoutes({ "GET /api/storage": fixtures.storage, ...over })

describe("Storage page", () => {
  it("is served inside Settings", async () => {
    mockApi(routes())
    renderApp("/settings/storage")
    expect(await screen.findByRole("heading", { level: 1, name: "Storage" })).toBeInTheDocument()
    expect(screen.getByRole("navigation", { name: "Settings" })).toBeInTheDocument()
  })

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

  it("lists every part of the disk under its bar, with Docker's counts and the build cache types", async () => {
    mockApi(routes())
    renderApp("/storage")
    const disk = within(await screen.findByRole("region", { name: "Disk" }))
    expect(disk.getByRole("img", { name: /^Disk use:/ })).toBeInTheDocument()
    expect(disk.queryByRole("list")).toBeNull()
    expect(disk.getByText("/var/lib/docker")).toHaveClass("font-mono")
    expect(disk.getByText("146.0 GB of 240.0 GB")).toHaveClass("font-mono")
    const table = within(disk.getByRole("table"))
    expect(table.getByRole("columnheader", { name: "Reclaimable" })).toHaveClass("text-right")
    const images = within(table.getByRole("rowheader", { name: "Images" }).closest("tr") as HTMLElement)
    expect(images.getByText("10")).toHaveClass("font-mono", "text-right")
    expect(images.getByText("8.1 GB")).toHaveClass("font-mono", "text-right", "whitespace-nowrap")
    expect(table.getByText("regular")).toBeInTheDocument()
    expect(table.getByRole("rowheader", { name: "Other" })).toBeInTheDocument()
    expect(table.getByRole("link", { name: "Toolchains" })).toHaveAttribute("href", "/settings/toolchains")
  })

  it("keeps what each Docker type can free under its size on a phone", async () => {
    mockApi(routes())
    renderApp("/storage")
    const table = within(within(await screen.findByRole("region", { name: "Disk" })).getByRole("table"))
    const images = within(table.getByRole("rowheader", { name: "Images" }).closest("tr") as HTMLElement)
    expect(images.getByText("7.1 GB reclaimable")).toHaveClass("sm:hidden")
    expect(images.getByText("7.1 GB").closest("td")).toHaveClass("hidden", "sm:table-cell")
  })

  it("is the target of the old prune link", async () => {
    mockApi(routes())
    renderApp("/storage")
    expect(await screen.findByRole("region", { name: "Disk" })).toHaveAttribute("id", "prune")
  })

  it("puts each targeted prune on the row it frees, none of them red", async () => {
    mockApi(routes())
    renderApp("/storage")
    const table = within(within(await screen.findByRole("region", { name: "Disk" })).getByRole("table"))
    const row = (name: string) => within(table.getByRole("rowheader", { name }).closest("tr") as HTMLElement)
    const trim = row("Build cache").getByRole("button", { name: "Trim to 20GB" })
    expect(trim).toBeEnabled()
    expect(row("Build cache").getByRole("button", { name: "Remove all build cache" })).toHaveTextContent(/^Remove all$/)
    expect(row("Images").getByRole("button", { name: "Remove dangling images" })).toBeInTheDocument()
    expect(row("Local volumes").getByRole("button", { name: "Remove unused volumes" })).toBeInTheDocument()
    expect(table.getByText("Unused volumes: refused while 1 job runs")).toBeInTheDocument()
    expect(row("Containers").queryByRole("button")).toBeNull()
    for (const b of table.getAllByRole("button", { name: /^(Trim|Remove)/ })) expect(b.className).not.toMatch(/destructive/)
  })

  it("folds a row's prunes into one menu for narrow screens", async () => {
    const { calls } = mockApi(routes({ "POST /api/prune/build-cache-keep": () => noContent() }))
    const { user } = renderApp("/storage")
    await user.click(await screen.findByRole("button", { name: "Prune build cache" }))
    expect(await screen.findByRole("menuitem", { name: "Remove all build cache" })).toBeInTheDocument()
    await user.click(screen.getByRole("menuitem", { name: "Trim to 20GB" }))
    await user.click(within(await screen.findByRole("alertdialog")).getByRole("button", { name: "Trim to 20GB" }))
    await waitFor(() => expect(calls.some((c) => c.method === "POST" && c.path === "/api/prune/build-cache-keep")).toBe(true))
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
    expect(screen.getByRole("button", { name: "Trim to 20GB" })).toBeDisabled()
    expect(screen.getByRole("button", { name: "Prune build cache" })).toBeDisabled()
  })

  it("says when nothing was pruned since the daemon started", async () => {
    mockApi(routes({ "GET /api/storage": { ...fixtures.storage, last_prune: null } }))
    renderApp("/storage")
    expect(within(await screen.findByRole("region", { name: "Activity" })).getByText("No prune since the daemon started")).toBeInTheDocument()
  })

  it("disables pruning while a prune runs", async () => {
    mockApi(routes({ "GET /api/status": { ...fixtures.status, maintenance: { running: true } } }))
    renderApp("/storage")
    expect(within(await screen.findByRole("region", { name: "Activity" })).getByText("Pruning")).toBeInTheDocument()
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
    ["Trim to 20GB", "Trim the build cache to 20GB?", "Build cache beyond that size is removed."],
    ["Remove all build cache", "Remove all build cache?", "Frees up to 380.0 MB. The next builds start cold."],
    ["Remove dangling images", "Remove dangling images?", "Images that are untagged and used by no container are removed."],
    ["Remove unused volumes", "Remove unused volumes?", "Every volume no container uses is removed. It is refused while a job runs or a runner starts, and idle runners stop while it runs."],
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
    await user.click(await screen.findByRole("button", { name: "Remove all build cache" }))
    await user.click(within(await screen.findByRole("alertdialog")).getByRole("button", { name: "Remove all build cache" }))
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

describe("Package caches", () => {
  it("lists the present caches and names the absent ones", async () => {
    mockApi(routes())
    renderApp("/storage")
    const caches = within(await screen.findByRole("region", { name: "Package caches" }))
    const row = within(caches.getByText("NuGet").closest("tr") as HTMLElement)
    expect(row.getAllByRole("cell")[0]).toHaveTextContent(/^NuGet\.nuget\/packages$/)
    expect(row.getByText(".nuget/packages")).toHaveClass("font-mono")
    expect(row.getByText("3.6 GB")).toHaveClass("font-mono", "whitespace-nowrap")
    expect(row.getByText("41,000")).toHaveClass("font-mono")
    expect(row.getByText("1h ago")).toHaveAttribute("title", "2026-10-03 13:05")
    expect(caches.getByText("Not present: Cargo")).toBeInTheDocument()
    expect(caches.queryByRole("button", { name: "Clear Cargo" })).toBeNull()
    expect(caches.getByText("Measured 14:03")).toBeInTheDocument()
    expect(caches.getByText("Clear: refused while 1 job runs")).toBeInTheDocument()
    expect(caches.queryByRole("img")).toBeNull()
    expect(caches.getByRole("columnheader", { name: "Last written" })).toHaveClass("hidden", "sm:table-cell")
  })

  it("names the day of a measurement before today", async () => {
    mockApi(routes({ "GET /api/storage": { ...fixtures.storage, measured_at: "2026-10-02T14:03:00Z" } }))
    renderApp("/storage")
    expect(await screen.findByText("Measured Oct 2 14:03")).toBeInTheDocument()
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
    expect(ask.getByText("Frees 3.6 GB. Jobs download what they need again. It is refused while a job runs or a runner starts, and idle runners stop while it runs.")).toBeInTheDocument()
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

describe("Activity", () => {
  it("leads with the last prune and what each step freed", async () => {
    mockApi(routes())
    renderApp("/storage")
    const activity = within(await screen.findByRole("region", { name: "Activity" }))
    expect(activity.getByText("Last prune: manual build-cache-all at 13:16, ok, 6.8 MB freed")).toBeInTheDocument()
    expect(activity.getByText("all build cache: 6.8 MB freed")).toHaveClass("text-muted-foreground")
  })

  it("shows a failed prune step in red", async () => {
    const last_prune = { ...(fixtures.storage.last_prune as LastPrune), steps: [{ name: "dangling images", freed: 0, error: "daemon busy" }] }
    mockApi(routes({ "GET /api/storage": { ...fixtures.storage, last_prune } }))
    renderApp("/storage")
    expect(await screen.findByText("dangling images: daemon busy")).toHaveClass("text-destructive")
  })

  it("names what runs and what waits in its header", async () => {
    mockApi(routes())
    renderApp("/storage")
    const activity = within(await screen.findByRole("region", { name: "Activity" }))
    expect(activity.getByText("Installing node 24, extracting, 1 more queued")).toBeInTheDocument()
  })

  it("lists finished operations with their outcome as a word", async () => {
    mockApi(routes())
    renderApp("/storage")
    const row = within((await screen.findByText("cleared NuGet (3.6 GB freed)")).closest("tr") as HTMLElement)
    expect(row.getByText("13:06")).toHaveClass("font-mono", "whitespace-nowrap")
    expect(row.getAllByRole("cell")[1]).toHaveTextContent(/^clear nugetcleared NuGet \(3\.6 GB freed\)$/)
    expectPill(row.getByText("OK"), "bg-success/15", "text-success")
    expect(row.getByText("cleared NuGet (3.6 GB freed)")).toHaveClass("text-muted-foreground")
    expect(row.getAllByRole("cell")).toHaveLength(3)
  })

  it("names the day of an operation before today", async () => {
    const recent = [{ ...(fixtures.storage.operations.recent?.[0] as Operation), finished_at: "2026-10-02T09:30:00Z" }]
    mockApi(routes({ "GET /api/storage": { ...fixtures.storage, operations: { ...fixtures.storage.operations, recent } } }))
    renderApp("/storage")
    expect(await screen.findByText("Oct 2 09:30")).toHaveAttribute("title", "2026-10-02 09:30")
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
