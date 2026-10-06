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
