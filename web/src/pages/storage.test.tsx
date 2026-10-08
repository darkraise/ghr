import { screen, waitFor, within } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import type { Storage } from "@/api/types"
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
