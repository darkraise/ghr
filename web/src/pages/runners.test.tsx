import { screen, waitFor, within } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import { json, mockApi, noContent } from "@/test/api"
import { authedRoutes, fixtures } from "@/test/fixtures"
import { setViewport } from "@/test/media"
import { renderApp } from "@/test/render"

const routes = (over: Record<string, unknown> = {}) =>
  authedRoutes({
    "GET /api/runners/aaaaaa/steps": fixtures.steps,
    "GET /api/runners/aaaaaa/containers": fixtures.containers,
    "GET /api/runners/aaaaaa/log": fixtures.log,
    "GET /api/runners/bbbbbb/steps": [],
    "GET /api/runners/bbbbbb/containers": [],
    "GET /api/runners/bbbbbb/log": { data: "bbbbbb says hi\n", next: "x" },
    ...over,
  })

const runnerLink = (id: string) => screen.findByRole("link", { name: new RegExp(`^${id}`) })

describe("Runners page", () => {
  it("sums up the runners in its header", async () => {
    mockApi(routes())
    renderApp("/runners")
    expect(await screen.findByText("2 live, 3 jobs waiting")).toBeInTheDocument()
  })

  it("shows only the list below 1024px", async () => {
    mockApi(routes())
    const { router } = renderApp("/runners")
    expect(await runnerLink("aaaaaa")).toBeInTheDocument()
    expect(screen.queryByRole("tab", { name: "Steps" })).toBeNull()
    expect(router.state.location.pathname).toBe("/runners")
  })

  it("shows only the panel with a way back below 1024px", async () => {
    mockApi(routes())
    renderApp("/runners/aaaaaa")
    expect(await screen.findByRole("heading", { name: "aaaaaa", level: 2 })).toBeInTheDocument()
    expect(screen.getByRole("link", { name: "Back to Runners" })).toBeInTheDocument()
    expect(screen.queryByRole("list", { name: "Runners" })).toBeNull()
  })

  it("selects the first runner in the URL and shows both halves at 1024px and wider", async () => {
    setViewport(1280)
    mockApi(routes())
    const { router } = renderApp("/runners")
    await waitFor(() => expect(router.state.location.pathname).toBe("/runners/aaaaaa"))
    expect(await screen.findByRole("heading", { name: "aaaaaa", level: 2 })).toBeInTheDocument()
    expect(await runnerLink("aaaaaa")).toHaveAttribute("aria-current", "page")
    expect(screen.queryByRole("link", { name: "Back to Runners" })).toBeNull()
  })

  it("keeps the selection when the selected runner finishes", { timeout: 10_000 }, async () => {
    setViewport(1280)
    let gone = false
    mockApi(
      routes({
        "GET /api/status": () => ({
          ...fixtures.status,
          instances: fixtures.status.instances.filter((i) => !(gone && i.id === "aaaaaa")),
        }),
      }),
    )
    const { router } = renderApp("/runners")
    await waitFor(() => expect(router.state.location.pathname).toBe("/runners/aaaaaa"))
    gone = true
    expect(await screen.findByText("This runner has finished")).toBeInTheDocument()
    expect(router.state.location.pathname).toBe("/runners/aaaaaa")
  })

  it("skips a cleaning runner when it picks the first one", async () => {
    setViewport(1280)
    const instances = fixtures.status.instances.map((i) => (i.id === "aaaaaa" ? { ...i, state: "cleaning" } : i))
    mockApi(routes({ "GET /api/status": { ...fixtures.status, instances } }))
    const { router } = renderApp("/runners")
    await waitFor(() => expect(router.state.location.pathname).toBe("/runners/bbbbbb"))
  })

  it("moves the panel with the arrow keys", async () => {
    setViewport(1280)
    mockApi(routes())
    const { router, user } = renderApp("/runners/aaaaaa")
    const first = await runnerLink("aaaaaa")
    first.focus()
    await user.keyboard("{ArrowDown}")
    await waitFor(() => expect(router.state.location.pathname).toBe("/runners/bbbbbb"))
    expect(await screen.findByRole("heading", { name: "bbbbbb", level: 2 })).toBeInTheDocument()
  })

  it("opens the log tab from a Dashboard link", async () => {
    mockApi(routes())
    renderApp("/runners/aaaaaa?tab=log")
    expect(await screen.findByText(/Listening for Jobs/)).toBeInTheDocument()
  })

  it("asks plainly before stopping an idle runner", async () => {
    mockApi(routes())
    const { user } = renderApp("/runners/bbbbbb")
    await user.click(await screen.findByRole("button", { name: "Stop runner" }))
    expect(await screen.findByText("Stop runner bbbbbb?")).toBeInTheDocument()
  })

  it("follows a runner that picks up a job while the dialog is open", { timeout: 10_000 }, async () => {
    let busy = false
    mockApi(
      routes({
        "GET /api/status": () => ({
          ...fixtures.status,
          instances: fixtures.status.instances.map((i) => (busy && i.id === "bbbbbb" ? { ...i, state: "busy" } : i)),
        }),
      }),
    )
    const { user } = renderApp("/runners/bbbbbb")
    await user.click(await screen.findByRole("button", { name: "Stop runner" }))
    expect(await screen.findByText("Stop runner bbbbbb?")).toBeInTheDocument()
    busy = true
    expect(await screen.findByText("Runner bbbbbb is running a job. Stop it?")).toBeInTheDocument()
  })

  it("says a runner that left while the dialog is open has finished", { timeout: 10_000 }, async () => {
    let gone = false
    const { calls } = mockApi(
      routes({
        "GET /api/status": () => ({
          ...fixtures.status,
          instances: fixtures.status.instances.filter((i) => !(gone && i.id === "bbbbbb")),
        }),
      }),
    )
    const { user } = renderApp("/runners/bbbbbb")
    await user.click(await screen.findByRole("button", { name: "Stop runner" }))
    expect(await screen.findByText("Stop runner bbbbbb?")).toBeInTheDocument()
    gone = true
    const ask = within(screen.getByRole("alertdialog"))
    expect(await ask.findByText("Runner bbbbbb has already finished")).toBeInTheDocument()
    expect(ask.getByRole("button", { name: "Stop" })).toBeDisabled()
    expect(calls.some((c) => c.method === "DELETE")).toBe(false)
  })

  it("toasts the runner it stopped", async () => {
    mockApi(routes({ "DELETE /api/runners/aaaaaa": () => noContent() }))
    const { user } = renderApp("/runners/aaaaaa")
    await user.click(await screen.findByRole("button", { name: "Stop runner" }))
    await user.click(within(await screen.findByRole("alertdialog")).getByRole("button", { name: "Stop" }))
    expect((await screen.findAllByText("Stopped aaaaaa")).length).toBeGreaterThan(0)
  })

  it("treats a runner the daemon no longer knows as finished", async () => {
    mockApi(routes({ "DELETE /api/runners/aaaaaa": () => json({ error: "runner aaaaaa not found" }, 404) }))
    const { user } = renderApp("/runners/aaaaaa")
    await user.click(await screen.findByRole("button", { name: "Stop runner" }))
    await user.click(within(await screen.findByRole("alertdialog")).getByRole("button", { name: "Stop" }))
    expect((await screen.findAllByText("aaaaaa had already finished")).length).toBeGreaterThan(0)
    expect(screen.queryByText("runner aaaaaa not found")).toBeNull()
  })

  it("keeps the runner's name in the dialog while it closes", async () => {
    mockApi(routes())
    const { user } = renderApp("/runners/bbbbbb")
    await user.click(await screen.findByRole("button", { name: "Stop runner" }))
    await screen.findByText("Stop runner bbbbbb?")
    await user.click(screen.getByRole("button", { name: "Cancel" }))
    expect(screen.queryByText(/undefined/)).toBeNull()
  })

  it("drops the snapshot when the page moves to another runner", async () => {
    let gone = false
    mockApi(
      routes({
        "GET /api/status": () =>
          gone ? { ...fixtures.status, instances: fixtures.status.instances.filter((i) => i.id !== "aaaaaa") } : fixtures.status,
      }),
    )
    const { router } = renderApp("/runners/aaaaaa")
    expect(await screen.findByText("Running for")).toBeInTheDocument()
    gone = true
    expect(await screen.findByText("This runner has finished")).toBeInTheDocument()
    await router.navigate({ to: "/runners/$id", params: { id: "zzzzzz" }, search: { tab: "steps" } })
    expect(await screen.findByRole("heading", { name: "zzzzzz" })).toBeInTheDocument()
    expect(screen.queryByText("Ran for")).toBeNull()
  })

  it("waits for the daemon while it cannot be reached", async () => {
    mockApi(routes({ "GET /api/status": () => json({ error: "connection refused" }, 502) }))
    renderApp("/runners")
    expect(await screen.findByText("Waiting for the daemon")).toBeInTheDocument()
  })
})
