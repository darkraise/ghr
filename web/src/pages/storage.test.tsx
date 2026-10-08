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
