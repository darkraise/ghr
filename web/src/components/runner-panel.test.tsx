import { useParams, useSearch } from "@tanstack/react-router"
import { fireEvent, screen, waitFor, within } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"
import { json, mockApi, noContent } from "@/test/api"
import { authedRoutes, fixtures } from "@/test/fixtures"
import { renderRoutes } from "@/test/routes"
import { RunnerPanel, type DetailTab } from "./runner-panel"

function PanelAt() {
  const { id } = useParams({ strict: false })
  const { tab } = useSearch({ strict: false }) as { tab?: DetailTab }
  return <RunnerPanel key={id} id={id ?? ""} tab={tab ?? "steps"} backLink />
}

const routes = (over: Record<string, unknown> = {}) =>
  authedRoutes({
    "GET /api/runners/aaaaaa/steps": fixtures.steps,
    "GET /api/runners/aaaaaa/containers": fixtures.containers,
    "GET /api/runners/aaaaaa/log": fixtures.log,
    ...over,
  })

const draw = (path = "/runners/aaaaaa?tab=steps") =>
  renderRoutes({ "/runners/$id": () => <PanelAt />, "/runners": () => <p>runner list</p> }, path)

afterEach(() => {
  Reflect.deleteProperty(HTMLElement.prototype, "scrollHeight")
  Reflect.deleteProperty(HTMLElement.prototype, "clientHeight")
  Reflect.deleteProperty(HTMLElement.prototype, "scrollTop")
  Reflect.deleteProperty(navigator, "clipboard")
  Reflect.deleteProperty(window, "isSecureContext")
})

describe("RunnerPanel", () => {
  it("shows the runner's facts, its steps and a link to its run", async () => {
    mockApi(routes())
    draw()
    expect(await screen.findByRole("heading", { name: "aaaaaa", level: 2 })).toBeInTheDocument()
    expect(await screen.findByText("Run tests")).toBeInTheDocument()
    expect(screen.getByText("Busy")).toHaveClass("text-primary")
    expect(screen.getByText("darkmem")).toBeInTheDocument()
    expect(screen.getByText("Running for")).toBeInTheDocument()
    expect(screen.getByRole("link", { name: "Open run" })).toHaveAttribute("href", "https://github.com/darkraise/darkmem/actions/runs/102/job/2")
    expect(screen.getByRole("link", { name: "Back to Runners" })).toHaveAttribute("href", "/runners")
    expect(screen.getByRole("img", { name: "Succeeded" })).toBeInTheDocument()
    expect(screen.getByRole("img", { name: "Pending" })).toBeInTheDocument()
  })

  it("follows the log on the Log tab and pauses when scrolled up", async () => {
    let top = 0
    Object.defineProperty(HTMLElement.prototype, "scrollHeight", { configurable: true, get: () => 500 })
    Object.defineProperty(HTMLElement.prototype, "clientHeight", { configurable: true, get: () => 100 })
    Object.defineProperty(HTMLElement.prototype, "scrollTop", {
      configurable: true,
      get: () => top,
      set: (v: number) => {
        top = v
      },
    })
    mockApi(routes())
    draw("/runners/aaaaaa?tab=log")
    const log = await screen.findByText(/Listening for Jobs/)
    expect(screen.getByRole("switch", { name: "Follow" })).toHaveAttribute("aria-checked", "true")
    top = 0
    fireEvent.scroll(log)
    await waitFor(() => expect(screen.getByRole("switch", { name: "Follow" })).toHaveAttribute("aria-checked", "false"))
  })

  it("lists the containers with their project", async () => {
    mockApi(routes())
    draw("/runners/aaaaaa?tab=containers")
    expect(await screen.findByText("ghr-aaaaaa-db-1")).toBeInTheDocument()
    expect(screen.getByText("ghr-aaaaaa")).toHaveClass("text-muted-foreground")
    expect(screen.getByText("Running")).toBeInTheDocument()
  })

  it("puts the chosen tab in the URL", async () => {
    mockApi(routes())
    const { router, user } = draw()
    await user.click(await screen.findByRole("tab", { name: "Log" }))
    await waitFor(() => expect(router.state.location.search).toEqual({ tab: "log" }))
  })

  it("stops the runner only after confirmation", async () => {
    const { calls } = mockApi(routes({ "DELETE /api/runners/aaaaaa": () => noContent() }))
    const { user } = draw()
    await screen.findByText("Busy")
    await user.click(screen.getByRole("button", { name: "Stop runner" }))
    expect(await screen.findByText("Runner aaaaaa is running a job. Stop it?")).toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: "Cancel" }))
    expect(calls.some((c) => c.method === "DELETE")).toBe(false)
    await user.click(screen.getByRole("button", { name: "Stop runner" }))
    await user.click(within(await screen.findByRole("alertdialog")).getByRole("button", { name: "Stop" }))
    await waitFor(() => expect(calls.some((c) => c.method === "DELETE" && c.path === "/api/runners/aaaaaa")).toBe(true))
  })

  it("keeps the last snapshot after the runner ends", async () => {
    let gone = false
    mockApi(
      routes({
        "GET /api/status": () =>
          gone ? { ...fixtures.status, instances: fixtures.status.instances.filter((i) => i.id !== "aaaaaa") } : fixtures.status,
      }),
    )
    draw()
    expect(await screen.findByText("Run tests")).toBeInTheDocument()
    gone = true
    expect(await screen.findByText("This runner has finished")).toBeInTheDocument()
    expect(screen.getByText("Finished")).toBeInTheDocument()
    expect(screen.getByText("Ran for")).toBeInTheDocument()
    expect(screen.getByText("Run tests")).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Stop runner" })).toBeDisabled()
  })

  it("says a runner never seen live keeps no steps", async () => {
    mockApi(routes({ "GET /api/runners/zzzzzz/log": fixtures.log }))
    const { user } = draw("/runners/zzzzzz?tab=steps")
    expect(await screen.findByText("Steps are not kept after a runner finishes.")).toBeInTheDocument()
    expect(screen.getByText("Finished")).toBeInTheDocument()
    await user.click(screen.getByRole("tab", { name: "Log" }))
    expect(await screen.findByText(/Listening for Jobs/)).toBeInTheDocument()
  })

  it("stays up while the daemon cannot be reached", async () => {
    mockApi(routes({ "GET /api/status": () => json({ error: "connection refused" }, 502) }))
    draw()
    expect(await screen.findByRole("heading", { name: "aaaaaa" })).toBeInTheDocument()
    expect(screen.queryByText("This runner has finished")).toBeNull()
    expect(screen.queryByText("Finished")).toBeNull()
    expect(screen.queryByText("Repository")).toBeNull()
    expect(screen.getByRole("button", { name: "Stop runner" })).toBeDisabled()
  })

  it("disables Stop when the daemon stops answering for a live runner", { timeout: 10_000 }, async () => {
    let down = false
    mockApi(routes({ "GET /api/status": () => (down ? json({ error: "connection refused" }, 502) : fixtures.status) }))
    draw()
    await screen.findByText("Busy")
    expect(screen.getByRole("button", { name: "Stop runner" })).toBeEnabled()
    down = true
    await waitFor(() => expect(screen.getByRole("button", { name: "Stop runner" })).toBeDisabled())
    expect(screen.getByText("Busy")).toBeInTheDocument()
  })

  it("says when no step is reported yet", async () => {
    mockApi(routes({ "GET /api/runners/aaaaaa/steps": [] }))
    draw()
    expect(await screen.findByText("No steps reported yet.")).toBeInTheDocument()
  })

  it("copies the run URL and the runner ID from its menu", async () => {
    mockApi(routes())
    const { user } = draw()
    Object.defineProperty(window, "isSecureContext", { value: true, configurable: true })
    const writeText = vi.fn(async () => {})
    Object.defineProperty(navigator, "clipboard", { value: { writeText }, configurable: true })
    await user.click(await screen.findByRole("button", { name: "More actions for runner aaaaaa" }))
    await user.click(await screen.findByRole("menuitem", { name: "Copy run URL" }))
    expect(writeText).toHaveBeenCalledWith("https://github.com/darkraise/darkmem/actions/runs/102/job/2")
    await user.click(screen.getByRole("button", { name: "More actions for runner aaaaaa" }))
    await user.click(await screen.findByRole("menuitem", { name: "Copy runner ID" }))
    expect(writeText).toHaveBeenCalledWith("aaaaaa")
  })
})
