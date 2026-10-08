import { screen, waitFor } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import type { LabelCheck } from "@/api/types"
import { mockApi } from "@/test/api"
import { authedRoutes, fixtures } from "@/test/fixtures"
import { renderApp } from "@/test/render"

const unmatched: LabelCheck = {
  state: "done",
  checked_at: new Date().toISOString(),
  partial: true,
  groups: [{ labels: ["self-hosted", "arm64", "big"], jobs: ["ci / build"], more: 2, count: 4, last_seen: new Date().toISOString() }],
}

function routes(over: Record<string, unknown> = {}) {
  return authedRoutes({
    "GET /api/history": [],
    "GET /api/repos/darkmem/registrations": [],
    "GET /api/repos/darkmem/label-check": fixtures.labelCheck,
    ...over,
  })
}

describe("Workflow labels card", () => {
  it("shows the label groups and whether ghr takes them", async () => {
    mockApi(routes())
    renderApp("/repositories/darkmem")
    expect(await screen.findByText("homelab, self-hosted")).toBeInTheDocument()
    expect(screen.getByText("matched")).toBeInTheDocument()
    expect(screen.getByText("ci / build, ci / test")).toBeInTheDocument()
    expect(screen.getByText(/^Checked /)).toBeInTheDocument()
  })

  it("offers the missing labels and adds one as an unsaved edit", async () => {
    const { calls } = mockApi(routes({ "GET /api/repos/darkmem/label-check": unmatched }))
    const { user } = renderApp("/repositories/darkmem")
    expect(await screen.findByText("missing arm64, big")).toBeInTheDocument()
    expect(screen.getByText(/ · partial$/)).toBeInTheDocument()
    expect(screen.getByText("ci / build +2 more")).toBeInTheDocument()
    expect(screen.getByText("needs a different OS or architecture")).toBeInTheDocument()
    expect(screen.queryByRole("button", { name: "+ add arm64" })).toBeNull()
    expect(
      screen.getByText("Adding a label changes which jobs ghr accepts; it does not install anything on the runner."),
    ).toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: "+ add big" }))
    expect(screen.getByRole("button", { name: "Remove big" })).toBeInTheDocument()
    expect(screen.getByText("1 unsaved change")).toBeInTheDocument()
    expect(calls.some((c) => c.method !== "GET")).toBe(false)
  })

  it("starts a check and polls it", async () => {
    let started = false
    const { calls } = mockApi(
      routes({
        "POST /api/repos/darkmem/label-check": () => {
          started = true
          return new Response(null, { status: 202 })
        },
        "GET /api/repos/darkmem/label-check": (): LabelCheck =>
          started ? { state: "checking", partial: false, groups: [] } : { state: "not_checked", partial: false, groups: [] },
      }),
    )
    const { user } = renderApp("/repositories/darkmem")
    expect(await screen.findByText("Not checked yet")).toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: "Check now" }))
    expect(await screen.findByText("Checking…")).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Check now" })).toBeDisabled()
    await waitFor(() => expect(calls.some((c) => c.method === "POST")).toBe(true))
  })

  it("does not read while GitHub rejects the token", async () => {
    const { calls } = mockApi(
      routes({ "GET /api/status": { ...fixtures.status, degraded: true, degraded_reason: "bad credentials" } }),
    )
    renderApp("/repositories/darkmem")
    expect((await screen.findAllByText("GitHub is rejecting the token: bad credentials")).length).toBeGreaterThan(0)
    expect(calls.some((c) => c.path.endsWith("/label-check"))).toBe(false)
  })
})
