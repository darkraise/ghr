import { screen, waitFor, within } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import { json, mockApi, noContent } from "@/test/api"
import { authedRoutes, fixtures } from "@/test/fixtures"
import { renderApp } from "@/test/render"

const detailRoutes = (over: Record<string, unknown> = {}) =>
  authedRoutes({
    "GET /api/runners/aaaaaa/steps": fixtures.steps,
    "GET /api/runners/aaaaaa/containers": fixtures.containers,
    "GET /api/runners/aaaaaa/log": fixtures.log,
    ...over,
  })

describe("runner detail page", () => {
  it("shows the runner and its steps", async () => {
    mockApi(detailRoutes())
    renderApp("/runners/aaaaaa")
    expect(await screen.findByRole("heading", { name: "aaaaaa" })).toBeInTheDocument()
    expect(await screen.findByText("Run tests")).toBeInTheDocument()
    expect(screen.getByText(/test #42/)).toBeInTheDocument()
    expect(screen.getByRole("link", { name: "Open run" })).toHaveAttribute(
      "href",
      "https://github.com/darkraise/darkmem/actions/runs/102/job/2",
    )
  })

  it("follows the log on the Log tab", async () => {
    mockApi(detailRoutes())
    renderApp("/runners/aaaaaa?tab=log")
    expect(await screen.findByText(/Listening for Jobs/)).toBeInTheDocument()
    expect(screen.getByRole("switch", { name: "Follow" })).toHaveAttribute("aria-checked", "true")
  })

  it("lists the containers", async () => {
    mockApi(detailRoutes())
    renderApp("/runners/aaaaaa?tab=containers")
    expect(await screen.findByText("ghr-aaaaaa-db-1")).toBeInTheDocument()
  })

  it("puts the chosen tab in the URL", async () => {
    mockApi(detailRoutes())
    const { user, router } = renderApp("/runners/aaaaaa")
    await user.click(await screen.findByRole("tab", { name: "Log" }))
    await waitFor(() => expect(router.state.location.search).toEqual({ tab: "log" }))
  })

  it("stops the runner only after confirmation", async () => {
    const { calls } = mockApi(detailRoutes({ "DELETE /api/runners/aaaaaa": () => noContent() }))
    const { user } = renderApp("/runners/aaaaaa")
    await user.click(await screen.findByRole("button", { name: "Stop runner" }))
    await user.click(screen.getByRole("button", { name: "Cancel" }))
    expect(calls.some((c) => c.method === "DELETE")).toBe(false)
    await user.click(screen.getByRole("button", { name: "Stop runner" }))
    await user.click(within(await screen.findByRole("alertdialog")).getByRole("button", { name: "Stop" }))
    await waitFor(() => expect(calls.some((c) => c.method === "DELETE" && c.path === "/api/runners/aaaaaa")).toBe(true))
  })

  it("keeps the last snapshot after the runner ends", async () => {
    let gone = false
    mockApi(
      detailRoutes({
        "GET /api/status": () =>
          gone ? { ...fixtures.status, instances: fixtures.status.instances.filter((i) => i.id !== "aaaaaa") } : fixtures.status,
      }),
    )
    renderApp("/runners/aaaaaa")
    expect(await screen.findByText("Run tests")).toBeInTheDocument()
    gone = true
    expect(await screen.findByText("This runner has finished", {}, { timeout: 3000 })).toBeInTheDocument()
    expect(screen.getByText(/test #42/)).toBeInTheDocument()
    expect(screen.getByText("Run tests")).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Stop runner" })).toBeDisabled()
  })

  it("keeps the page up while the daemon cannot be reached", async () => {
    mockApi(detailRoutes({ "GET /api/status": () => json({ error: "connection refused" }, 502) }))
    renderApp("/runners/aaaaaa")
    expect(await screen.findByRole("heading", { name: "aaaaaa" })).toBeInTheDocument()
    expect(await screen.findByText("daemon unreachable: connection refused — retrying")).toBeInTheDocument()
    expect(screen.queryByText("This runner has finished")).toBeNull()
    expect(screen.getByRole("button", { name: "Stop runner" })).toBeDisabled()
  })

  it("shows the steps' empty state", async () => {
    mockApi(detailRoutes({ "GET /api/runners/aaaaaa/steps": [] }))
    renderApp("/runners/aaaaaa")
    expect(await screen.findByText("no steps reported yet")).toBeInTheDocument()
  })
})
