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

  it("disables pruning while the daemon is unreachable", { timeout: 10_000 }, async () => {
    let reads = 0
    mockApi(routes({ "GET /api/status": () => (++reads === 1 ? fixtures.status : json({ error: "connection refused" }, 502)) }))
    renderApp("/storage")
    expect(await screen.findByText("Reconnecting", {}, { timeout: 4000 })).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Prune" })).toBeDisabled()
  })

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
