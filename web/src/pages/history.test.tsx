import { screen, waitFor, within } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"
import type { HistoryEntry } from "@/api/types"
import { json, mockApi } from "@/test/api"
import { authedRoutes, fixtures } from "@/test/fixtures"
import { renderApp } from "@/test/render"

const secureContext = Object.getOwnPropertyDescriptor(window, "isSecureContext")

afterEach(() => {
  Reflect.deleteProperty(navigator, "clipboard")
  if (secureContext) Object.defineProperty(window, "isSecureContext", secureContext)
  else Reflect.deleteProperty(window, "isSecureContext")
})

const SINCE = "?repo=&conclusion=&since=2026-10-02T14%3A00%3A00Z&limit=500"
const historyCalls = (calls: { path: string; search: string }[]) => calls.filter((c) => c.path === "/api/history").map((c) => c.search)
const activityCalls = (calls: { path: string; search: string }[]) => calls.filter((c) => c.path === "/api/activity").map((c) => c.search)

describe("History page", () => {
  it("lists finished jobs under day headings and sums them up", async () => {
    mockApi(authedRoutes({ "GET /api/history": fixtures.history }))
    renderApp("/history")
    expect(await screen.findByText("build")).toBeInTheDocument()
    const table = within(screen.getByRole("table"))
    const today = table.getByText("Today")
    expect(today.tagName).toBe("TH")
    expect(today).toHaveAttribute("scope", "rowgroup")
    expect(table.getByText("#7")).toBeInTheDocument()
    expect(table.getAllByText("deploy")).toHaveLength(2)
    expect(table.getByText("#41")).toBeInTheDocument()
    expect(table.getByText("Succeeded")).toBeInTheDocument()
    expect(table.getByText("Failed")).toBeInTheDocument()
    expect(screen.getByText("2 jobs, 1 failed, median 3m15s")).toBeInTheDocument()
    expect(screen.getAllByRole("link", { name: /^Open run/ })).toHaveLength(1)
  })

  it("reads the table from where the chart starts", async () => {
    const { calls } = mockApi(authedRoutes({ "GET /api/history": fixtures.history }))
    renderApp("/history")
    await screen.findByText("build")
    expect(activityCalls(calls)[0]).toBe("?window=7d&tz=UTC")
    expect(historyCalls(calls)[0]).toBe(SINCE)
    expect(screen.getByRole("group", { name: "Jobs per bucket for the last 24 hours" })).toBeInTheDocument()
  })

  it("filters by repository through the URL", async () => {
    const { calls } = mockApi(authedRoutes({ "GET /api/history": fixtures.history }))
    const { user, router } = renderApp("/history")
    await screen.findByText("build")
    await user.click(screen.getByRole("combobox", { name: "Repository" }))
    await user.click(await screen.findByRole("option", { name: "darkmem" }))
    await waitFor(() => expect(router.state.location.search).toEqual({ repo: "darkmem" }))
    await waitFor(() => expect(activityCalls(calls)).toContain("?window=7d&tz=UTC&repo=darkmem"))
    await waitFor(() => expect(historyCalls(calls)).toContain("?repo=darkmem&conclusion=&since=2026-10-02T14%3A00%3A00Z&limit=500"))
  })

  it("hides the previous filter's rows while the next ones load", { timeout: 10_000 }, async () => {
    let release = () => {}
    const gate = new Promise<void>((resolve) => (release = resolve))
    let reads = 0
    mockApi(
      authedRoutes({
        "GET /api/history": async () => {
          reads += 1
          if (reads > 1) await gate
          return fixtures.history
        },
      }),
    )
    const { user } = renderApp("/history")
    await screen.findByText("#7")
    await user.click(screen.getByRole("combobox", { name: "Repository" }))
    await user.click(await screen.findByRole("option", { name: "darkmem" }))
    await waitFor(() => expect(screen.queryByText("#7")).toBeNull())
    expect(screen.queryByText("2 jobs, 1 failed, median 3m15s")).toBeNull()
    release()
    expect(await screen.findByText("#7")).toBeInTheDocument()
  })

  it("says the table waits for the chart when the chart cannot load", async () => {
    mockApi(authedRoutes({ "GET /api/activity": () => json({ error: "activity unavailable" }, 500) }))
    renderApp("/history")
    expect(await screen.findByText("The table loads once the chart does.")).toBeInTheDocument()
  })

  it("filters by result, and a second click goes back to all", async () => {
    const { calls } = mockApi(authedRoutes({ "GET /api/history": fixtures.history }))
    const { user, router } = renderApp("/history")
    await screen.findByText("build")
    await user.click(screen.getByRole("radio", { name: "Failed" }))
    await waitFor(() => expect(router.state.location.search).toEqual({ result: "failure" }))
    await waitFor(() => expect(historyCalls(calls)).toContain("?repo=&conclusion=failure&since=2026-10-02T14%3A00%3A00Z&limit=500"))
    await user.click(screen.getByRole("radio", { name: "Failed" }))
    await waitFor(() => expect(router.state.location.search).toEqual({}))
    expect(screen.getByRole("radio", { name: "All" })).toBeChecked()
  })

  it("changes the window", async () => {
    const { calls } = mockApi(authedRoutes({ "GET /api/history": fixtures.history }))
    const { user, router } = renderApp("/history")
    await screen.findByText("build")
    await user.click(screen.getByRole("radio", { name: "24h" }))
    await waitFor(() => expect(router.state.location.search).toEqual({ window: "24h" }))
    await waitFor(() => expect(activityCalls(calls)).toContain("?window=24h&tz=UTC"))
  })

  it("restores its controls from the URL", async () => {
    const { calls } = mockApi(authedRoutes({ "GET /api/history": [] }))
    renderApp("/history?repo=old-repo&result=failure&window=30d")
    await waitFor(() => expect(activityCalls(calls)).toContain("?window=30d&tz=UTC&repo=old-repo"))
    expect(screen.getByRole("radio", { name: "Failed" })).toBeChecked()
    expect(screen.getByRole("radio", { name: "30d" })).toBeChecked()
    expect(screen.getByRole("combobox", { name: "Repository" })).toHaveTextContent("old-repo")
  })

  it("falls back to its defaults for values it does not know", async () => {
    const { calls } = mockApi(authedRoutes({ "GET /api/history": [] }))
    renderApp("/history?window=2h&result=bogus")
    await waitFor(() => expect(activityCalls(calls)).toContain("?window=7d&tz=UTC"))
    expect(screen.getByRole("radio", { name: "All" })).toBeChecked()
    expect(screen.getByRole("radio", { name: "7d" })).toBeChecked()
  })

  it("narrows the table to a picked bucket, by mouse or keyboard", async () => {
    mockApi(authedRoutes({ "GET /api/history": fixtures.history }))
    const { user } = renderApp("/history")
    await screen.findByText("#7")
    const bucket = await screen.findByRole("button", { name: /^13:00 to 14:00/ })
    await user.click(bucket)
    expect(bucket).toHaveAttribute("aria-pressed", "true")
    expect(await screen.findByText("Showing 13:00 to 14:00, Oct 3")).toBeInTheDocument()
    expect(screen.getByText("#41")).toBeInTheDocument()
    expect(screen.queryByText("#7")).toBeNull()
    await user.click(screen.getByRole("button", { name: "Show whole window" }))
    expect(await screen.findByText("#7")).toBeInTheDocument()
    bucket.focus()
    await user.keyboard("{Enter}")
    expect(await screen.findByText("Showing 13:00 to 14:00, Oct 3")).toBeInTheDocument()
    await user.keyboard(" ")
    expect(await screen.findByText("#7")).toBeInTheDocument()
  })

  // Rendering 500 rows in jsdom takes about 13 s on a heavily loaded host.
  it("says when the 500-job cap cut the window short", { timeout: 30_000 }, async () => {
    const [build] = fixtures.history as [HistoryEntry]
    const many = Array.from({ length: 500 }, (_, i) => ({ ...build, id: `h${i}` }))
    mockApi(authedRoutes({ "GET /api/history": many }))
    renderApp("/history")
    expect(await screen.findByText("Showing the newest 500 jobs in this window.")).toBeInTheDocument()
  })

  it("says when nothing finished in the window", async () => {
    mockApi(authedRoutes({ "GET /api/history": [] }))
    renderApp("/history")
    expect(await screen.findByText("No finished jobs in the last 7 days")).toBeInTheDocument()
  })

  it("offers to clear filters that match nothing", async () => {
    mockApi(authedRoutes({ "GET /api/history": [] }))
    const { user, router } = renderApp("/history?result=failure")
    expect(await screen.findByText("No jobs match these filters")).toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: "Clear filters" }))
    await waitFor(() => expect(router.state.location.search).toEqual({}))
  })

  it("shows a failed read", async () => {
    mockApi(authedRoutes({ "GET /api/history": () => json({ error: "history file unreadable" }, 500) }))
    renderApp("/history")
    expect(within(await screen.findByRole("alert")).getByText("history file unreadable")).toBeInTheDocument()
  })

  it("shows loading until the chart's span is known", async () => {
    mockApi(authedRoutes({ "GET /api/activity": () => new Promise(() => {}) }))
    renderApp("/history")
    expect((await screen.findAllByText("Loading")).length).toBeGreaterThan(0)
    expect(screen.queryByText(/^No finished jobs/)).toBeNull()
  })

  it("copies a run's URL", async () => {
    mockApi(authedRoutes({ "GET /api/history": fixtures.history }))
    const { user } = renderApp("/history")
    Object.defineProperty(window, "isSecureContext", { value: true, configurable: true })
    const writeText = vi.fn(async () => {})
    Object.defineProperty(navigator, "clipboard", { value: { writeText }, configurable: true })
    await user.click(await screen.findByRole("button", { name: "Copy run URL of build #41" }))
    expect(writeText).toHaveBeenCalledWith("https://github.com/darkraise/darkmem/actions/runs/101/job/1")
    expect((await screen.findAllByText("Copied run URL")).length).toBeGreaterThan(0)
  })
})
