import { screen, waitFor, within } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"
import { json, mockApi, noContent } from "@/test/api"
import { authedRoutes, fixtures } from "@/test/fixtures"
import { renderApp } from "@/test/render"

const secureContext = Object.getOwnPropertyDescriptor(window, "isSecureContext")

afterEach(() => {
  Reflect.deleteProperty(navigator, "clipboard")
  if (secureContext) Object.defineProperty(window, "isSecureContext", secureContext)
  else Reflect.deleteProperty(window, "isSecureContext")
})

async function rowOf(id: string) {
  const cell = await screen.findByRole("link", { name: id })
  const row = cell.closest("tr")
  if (!row) throw new Error(`no row for ${id}`)
  return within(row)
}

describe("Runners page", () => {
  it("lists runners with their state and job", async () => {
    mockApi(authedRoutes())
    renderApp("/runners")
    const busy = await rowOf("aaaaaa")
    expect(busy.getByText("busy")).toBeInTheDocument()
    expect(busy.getByText("test #42")).toBeInTheDocument()
    const idle = await rowOf("bbbbbb")
    expect(idle.getByText("idle")).toBeInTheDocument()
  })

  it("names each row's buttons after its runner", async () => {
    mockApi(authedRoutes())
    renderApp("/runners")
    const row = await rowOf("aaaaaa")
    expect(row.getByRole("button", { name: "Logs for runner aaaaaa" })).toHaveTextContent("Logs")
    expect(row.getByRole("button", { name: "Copy ID of runner aaaaaa" })).toHaveTextContent("Copy ID")
    expect(row.getByRole("button", { name: "Stop runner aaaaaa" })).toHaveTextContent("Stop")
  })

  it("says when no runner is up", async () => {
    mockApi(authedRoutes({ "GET /api/status": { ...fixtures.status, instances: [], repos: [] } }))
    renderApp("/runners")
    expect(await screen.findByText("no runners — they start when jobs are queued")).toBeInTheDocument()
  })

  it("shows repos waiting on their cap", async () => {
    const repos = [{ name: "darkmem", paused: false, max: 1, active: 1, queued: 2 }]
    mockApi(authedRoutes({ "GET /api/status": { ...fixtures.status, instances: [], repos } }))
    renderApp("/runners")
    expect(await screen.findByText("2 jobs queued (repo cap 1)")).toBeInTheDocument()
  })

  it("waits for the daemon while it cannot be reached", async () => {
    mockApi(authedRoutes({ "GET /api/status": () => json({ error: "connection refused" }, 502) }))
    renderApp("/runners")
    expect(await screen.findByText("waiting for the daemon…")).toBeInTheDocument()
  })

  it("stops a runner only after confirmation", async () => {
    const { calls } = mockApi(authedRoutes({ "DELETE /api/runners/aaaaaa": () => noContent() }))
    const { user } = renderApp("/runners")
    const row = await rowOf("aaaaaa")

    await user.click(row.getByRole("button", { name: "Stop runner aaaaaa" }))
    expect(await screen.findByText("Runner aaaaaa is running a job. Stop it?")).toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: "Cancel" }))
    expect(calls.some((c) => c.method === "DELETE")).toBe(false)

    await user.click(row.getByRole("button", { name: "Stop runner aaaaaa" }))
    await user.click(within(await screen.findByRole("alertdialog")).getByRole("button", { name: "Stop" }))
    await waitFor(() => expect(calls.some((c) => c.method === "DELETE" && c.path === "/api/runners/aaaaaa")).toBe(true))
    await waitFor(() => expect(screen.queryByRole("alertdialog")).toBeNull())
    expect((await screen.findAllByText("stopped aaaaaa")).length).toBeGreaterThan(0)
  })

  it("asks plainly before stopping an idle runner", async () => {
    mockApi(authedRoutes())
    const { user } = renderApp("/runners")
    await user.click((await rowOf("bbbbbb")).getByRole("button", { name: "Stop runner bbbbbb" }))
    expect(await screen.findByText("Stop runner bbbbbb?")).toBeInTheDocument()
  })

  it("follows a runner that picks up a job while the dialog is open", { timeout: 10_000 }, async () => {
    let busy = false
    mockApi(
      authedRoutes({
        "GET /api/status": () => ({
          ...fixtures.status,
          instances: fixtures.status.instances.map((i) => (busy && i.id === "bbbbbb" ? { ...i, state: "busy" } : i)),
        }),
      }),
    )
    const { user } = renderApp("/runners")
    await user.click((await rowOf("bbbbbb")).getByRole("button", { name: "Stop runner bbbbbb" }))
    expect(await screen.findByText("Stop runner bbbbbb?")).toBeInTheDocument()
    busy = true
    expect(await screen.findByText("Runner bbbbbb is running a job. Stop it?", {}, { timeout: 3000 })).toBeInTheDocument()
  })

  it("keeps the runner's name in the dialog while it closes", async () => {
    mockApi(authedRoutes())
    const { user } = renderApp("/runners")
    await user.click((await rowOf("bbbbbb")).getByRole("button", { name: "Stop runner bbbbbb" }))
    await screen.findByText("Stop runner bbbbbb?")
    await user.click(screen.getByRole("button", { name: "Cancel" }))
    expect(screen.queryByText(/undefined/)).toBeNull()
  })

  it("copies a runner ID", async () => {
    mockApi(authedRoutes())
    const { user } = renderApp("/runners")
    // userEvent.setup() (inside renderApp) installs its own clipboard stub, so the mock goes in after it.
    Object.defineProperty(window, "isSecureContext", { value: true, configurable: true })
    const writeText = vi.fn(async () => {})
    Object.defineProperty(navigator, "clipboard", { value: { writeText }, configurable: true })
    await user.click((await rowOf("aaaaaa")).getByRole("button", { name: "Copy ID of runner aaaaaa" }))
    expect(writeText).toHaveBeenCalledWith("aaaaaa")
    expect((await screen.findAllByText("copied aaaaaa")).length).toBeGreaterThan(0)
  })

  it("opens the log tab from Logs", async () => {
    mockApi(authedRoutes())
    const { user, router } = renderApp("/runners")
    await user.click((await rowOf("aaaaaa")).getByRole("button", { name: "Logs for runner aaaaaa" }))
    await waitFor(() => expect(router.state.location.pathname).toBe("/runners/aaaaaa"))
    expect(router.state.location.search).toEqual({ tab: "log" })
  })
})
