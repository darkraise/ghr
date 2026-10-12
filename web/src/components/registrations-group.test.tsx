import { screen, waitFor, within } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import type { Registration } from "@/api/types"
import { json, mockApi, noContent } from "@/test/api"
import { authedRoutes, fixtures } from "@/test/fixtures"
import { expectPill } from "@/test/pill"
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

describe("GitHub registrations group", () => {
  it("lists registrations and offers Delete only for an offline foreign one", async () => {
    mockApi(routes())
    renderApp("/repositories/darkmem/registrations")
    expect(await screen.findByText("ghr-aaaaaa")).toBeInTheDocument()
    expect(screen.getByText("old-runner")).toBeInTheDocument()
    expect(screen.getByText("self-hosted linux")).toBeInTheDocument()
    const region = within(screen.getByRole("region", { name: "GitHub registrations" }))
    expectPill(region.getByText("Offline"), "bg-destructive/15", "text-destructive")
    expectPill(region.getByText("Running"), "bg-running/15", "text-running")
    expect(region.getByText("ghr")).toHaveClass("text-muted-foreground")
    expect(region.getByText("self-hosted linux")).toHaveClass("font-mono")
    expect(screen.getByRole("button", { name: "Delete old-runner" })).toBeInTheDocument()
    expect(screen.queryByRole("button", { name: "Delete ghr-aaaaaa" })).toBeNull()
  })

  it("deletes a registration only after confirmation", async () => {
    const { calls } = mockApi(routes({ "DELETE /api/repos/darkmem/registrations/9": () => noContent() }))
    const { user } = renderApp("/repositories/darkmem/registrations")
    await user.click(await screen.findByRole("button", { name: "Delete old-runner" }))
    const ask = within(await screen.findByRole("alertdialog"))
    expect(ask.getByText("Delete runner registration old-runner?")).toBeInTheDocument()
    expect(ask.getByText("It is removed from darkmem on GitHub.")).toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: "Cancel" }))
    await waitFor(() => expect(screen.queryByRole("alertdialog")).toBeNull())
    expect(calls.some((c) => c.method === "DELETE")).toBe(false)

    await user.click(screen.getByRole("button", { name: "Delete old-runner" }))
    await user.click(within(await screen.findByRole("alertdialog")).getByRole("button", { name: "Delete" }))
    await waitFor(() =>
      expect(calls.some((c) => c.method === "DELETE" && c.path === "/api/repos/darkmem/registrations/9")).toBe(true),
    )
    expect((await screen.findAllByText("Deleted old-runner")).length).toBeGreaterThan(0)
  })

  it("says when nothing is registered, and refreshes", async () => {
    const { calls } = mockApi(routes({ "GET /api/repos/darkmem/registrations": [] }))
    const { user } = renderApp("/repositories/darkmem/registrations")
    expect(
      await screen.findByText("No runners registered. ghr starts single-use runners on demand, and keeps warm ones in all mode."),
    ).toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: "Refresh" }))
    await waitFor(() => expect(calls.filter((c) => c.path === "/api/repos/darkmem/registrations")).toHaveLength(2))
  })

  it("keeps the list when a read fails, with a way to retry", async () => {
    let down = false
    mockApi(
      routes({
        "GET /api/repos/darkmem/registrations": () =>
          down ? json({ error: "GitHub is not answering" }, 502) : [...fixtures.registrations, stale],
      }),
    )
    const { user } = renderApp("/repositories/darkmem/registrations")
    expect(await screen.findByText("old-runner")).toBeInTheDocument()
    down = true
    await user.click(screen.getByRole("button", { name: "Refresh" }))
    const region = within(screen.getByRole("region", { name: "GitHub registrations" }))
    expect(await region.findByText("GitHub is not answering")).toBeInTheDocument()
    expect(region.getByText("old-runner")).toBeInTheDocument()
    down = false
    await user.click(region.getByRole("button", { name: "Try again" }))
    await waitFor(() => expect(region.queryByText("GitHub is not answering")).toBeNull())
    expect(region.getByText("old-runner")).toBeInTheDocument()
  })

  it("does not read while GitHub rejects the token", async () => {
    const { calls } = mockApi(
      routes({ "GET /api/status": { ...fixtures.status, degraded: true, degraded_reason: "bad credentials" } }),
    )
    renderApp("/repositories/darkmem/registrations")
    expect(await screen.findByText("GitHub is rejecting the token: bad credentials")).toBeInTheDocument()
    expect(calls.some((c) => c.path.endsWith("/registrations"))).toBe(false)
  })
})
