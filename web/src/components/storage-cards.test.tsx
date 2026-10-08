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
    expect(screen.getByText("Clear: refused while 1 job runs")).toBeInTheDocument()
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
    expect(screen.queryByText("clear nuget: cleared NuGet (3.6 GB freed)")).toBeNull()
    const toasts = await screen.findAllByText(
      "remove go 1.23.1: permission denied · install node 24: installed node 24.9.0",
      undefined,
      { timeout: 4000 },
    )
    expect(toasts.length).toBeGreaterThan(0)
  })
})
