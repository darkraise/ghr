import { useParams, useSearch } from "@tanstack/react-router"
import { act, fireEvent, screen, waitFor, within } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { afterEach, describe, expect, it, vi } from "vitest"
import { json, mockApi, noContent } from "@/test/api"
import { authedRoutes, fixtures } from "@/test/fixtures"
import { expectPill } from "@/test/pill"
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
    expectPill(screen.getAllByText("Running")[0] as HTMLElement, "bg-running/15", "text-running")
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

  it("does not claim an empty log while it loads", async () => {
    mockApi(routes({ "GET /api/runners/aaaaaa/log": () => new Promise(() => {}) }))
    draw("/runners/aaaaaa?tab=log")
    expect((await screen.findAllByText("Loading")).length).toBeGreaterThan(0)
    expect(screen.queryByText("No log output yet")).toBeNull()
  })

  it("offers a retry when the log cannot be read", async () => {
    let fail = true
    mockApi(routes({ "GET /api/runners/aaaaaa/log": () => (fail ? json({ error: "log unavailable" }, 500) : fixtures.log) }))
    draw("/runners/aaaaaa?tab=log")
    const retry = await screen.findByRole("button", { name: "Try again" })
    fail = false
    await userEvent.setup().click(retry)
    expect(await screen.findByText(/Listening for Jobs/)).toBeInTheDocument()
  })

  it("stops polling a finished runner's log and keeps what it read", { timeout: 15_000 }, async () => {
    let gone = false
    const { calls } = mockApi(
      routes({
        "GET /api/status": () =>
          gone ? { ...fixtures.status, instances: fixtures.status.instances.filter((i) => i.id !== "aaaaaa") } : fixtures.status,
        "GET /api/runners/aaaaaa/log": ({ url }: { url: URL }) => {
          const cursor = url.searchParams.get("cursor") ?? ""
          return cursor ? { data: "", next: cursor } : fixtures.log
        },
      }),
    )
    draw("/runners/aaaaaa?tab=log")
    await screen.findByText(/Listening for Jobs/)
    gone = true
    await screen.findByText("This runner has finished")
    await new Promise((resolve) => setTimeout(resolve, 1500))
    const logCalls = () => calls.filter((c) => c.path === "/api/runners/aaaaaa/log").length
    const before = logCalls()
    await new Promise((resolve) => setTimeout(resolve, 2500))
    expect(logCalls()).toBe(before)
    expect(screen.getByText(/Listening for Jobs/)).toBeInTheDocument()
  })

  it("lists the containers with their project", async () => {
    mockApi(routes())
    draw("/runners/aaaaaa?tab=containers")
    expect(await screen.findByText("ghr-aaaaaa-db-1")).toBeInTheDocument()
    expect(screen.getByText("ghr-aaaaaa")).toHaveClass("text-muted-foreground")
    const row = within(screen.getByText("ghr-aaaaaa-db-1").closest("tr") as HTMLElement)
    expect(row.getByText("Running")).toBeInTheDocument()
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
    await screen.findByText("Running")
    await user.click(screen.getByRole("button", { name: "Stop runner" }))
    expect(await screen.findByText("Runner aaaaaa is running a job. Stop it?")).toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: "Cancel" }))
    expect(calls.some((c) => c.method === "DELETE")).toBe(false)
    await user.click(screen.getByRole("button", { name: "Stop runner" }))
    await user.click(within(await screen.findByRole("alertdialog")).getByRole("button", { name: "Stop" }))
    await waitFor(() => expect(calls.some((c) => c.method === "DELETE" && c.path === "/api/runners/aaaaaa")).toBe(true))
  })

  it("says a stopping runner is stopping and offers no second stop", async () => {
    mockApi(
      routes({
        "GET /api/status": {
          ...fixtures.status,
          instances: fixtures.status.instances.map((i) => (i.id === "aaaaaa" ? { ...i, stopping: true } : i)),
        },
      }),
    )
    draw()
    expectPill(await screen.findByText("Stopping"), "bg-muted-foreground/15", "text-muted-foreground")
    expect(screen.queryByText("Running")).toBeNull()
    expect(screen.getByRole("button", { name: "Stop runner" })).toBeDisabled()
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

  it("stops counting a step's time once the runner ends", { timeout: 15_000 }, async () => {
    vi.useFakeTimers({ toFake: ["Date"], now: Date.parse("2026-10-03T14:05:00Z") })
    try {
      let gone = false
      mockApi(
        routes({
          "GET /api/status": () => ({
            ...fixtures.status,
            now: new Date().toISOString(),
            instances: gone ? fixtures.status.instances.filter((i) => i.id !== "aaaaaa") : fixtures.status.instances,
          }),
        }),
      )
      draw()
      const took = async () => within((await screen.findByText("Run tests")).closest("li") as HTMLElement).getByText(/^\d+m\d{2}s$/).textContent
      expect(await took()).toBe("3m54s")
      gone = true
      await screen.findByText("This runner has finished")
      await new Promise((resolve) => setTimeout(resolve, 1200))
      const frozen = await took()
      vi.setSystemTime(Date.now() + 60_000)
      await new Promise((resolve) => setTimeout(resolve, 1200))
      expect(await took()).toBe(frozen)
      expect(screen.getByRole("img", { name: "Last seen running" })).toBeInTheDocument()
    } finally {
      vi.useRealTimers()
    }
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
    await screen.findByText("Running")
    expect(screen.getByRole("button", { name: "Stop runner" })).toBeEnabled()
    down = true
    await waitFor(() => expect(screen.getByRole("button", { name: "Stop runner" })).toBeDisabled())
    expect(screen.getAllByText("Running").length).toBeGreaterThan(0)
  })

  it.each<[string, string, string, string]>([
    ["steps", "/runners/aaaaaa?tab=steps", "GET /api/runners/aaaaaa/steps", "Run tests"],
    ["containers", "/runners/aaaaaa?tab=containers", "GET /api/runners/aaaaaa/containers", "ghr-aaaaaa-db-1"],
  ])("keeps the %s on screen when a poll fails, with a way to retry", async (tab, path, route, shown) => {
    let down = false
    mockApi(routes({ [route]: () => (down ? json({ error: "GitHub is not answering" }, 502) : fixtures[tab as "steps" | "containers"]) }))
    const { queryClient, user } = draw(path)
    expect(await screen.findByText(shown)).toBeInTheDocument()
    down = true
    await act(() => queryClient.refetchQueries({ queryKey: [tab, "aaaaaa"] }))
    expect(await screen.findByText("GitHub is not answering")).toBeInTheDocument()
    expect(screen.getByText(shown)).toBeInTheDocument()
    down = false
    await user.click(screen.getByRole("button", { name: "Try again" }))
    await waitFor(() => expect(screen.queryByText("GitHub is not answering")).toBeNull())
    expect(screen.getByText(shown)).toBeInTheDocument()
  })

  it("reads steps and containers only while their own tab is open", async () => {
    const { calls } = mockApi(routes())
    const { queryClient, user } = draw("/runners/aaaaaa?tab=steps")
    const reads = (what: string) => calls.filter((c) => c.path === `/api/runners/aaaaaa/${what}`).length
    const polled = (what: string) => queryClient.getQueryCache().find({ queryKey: [what, "aaaaaa"] })?.isActive() ?? false
    expect(await screen.findByText("Run tests")).toBeInTheDocument()
    expect(reads("containers")).toBe(0)
    expect([polled("steps"), polled("containers")]).toEqual([true, false])

    await user.click(screen.getByRole("tab", { name: "Containers" }))
    expect(await screen.findByText("ghr-aaaaaa-db-1")).toBeInTheDocument()
    expect([polled("steps"), polled("containers")]).toEqual([false, true])

    await user.click(screen.getByRole("tab", { name: "Log" }))
    expect(await screen.findByText(/Listening for Jobs/)).toBeInTheDocument()
    expect([polled("steps"), polled("containers")]).toEqual([false, false])

    const before = reads("steps")
    await user.click(screen.getByRole("tab", { name: "Steps" }))
    await waitFor(() => expect(reads("steps")).toBe(before + 1))
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
