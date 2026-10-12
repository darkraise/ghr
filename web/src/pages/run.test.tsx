import { act, screen, waitFor, within } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"
import { renderAnsi } from "@/lib/ansi"
import { json, mockApi, type Call } from "@/test/api"
import { fixtures, runRoutes } from "@/test/fixtures"
import { renderApp } from "@/test/render"

// Every log line and every group header draws its text through renderAnsi,
// so its calls count how many of them rendered.
vi.mock("@/lib/ansi", async (importOriginal) => {
  const mod = await importOriginal<typeof import("@/lib/ansi")>()
  return { ...mod, renderAnsi: vi.fn(mod.renderAnsi) }
})

afterEach(() => {
  vi.useRealTimers()
})

const runCalls = (calls: Call[]) => calls.filter((c) => c.path.startsWith("/api/actions/"))

// The page's clock follows the daemon's, so under fake timers the daemon's
// time has to move with them.
const daemonClock = () => ({ ...fixtures.status, now: new Date().toISOString() })

describe("Run page", () => {
  it("shows the run's header and summary facts", async () => {
    mockApi(runRoutes())
    renderApp("/actions/darkmem/runs/102")
    expect(await screen.findByRole("heading", { name: "Fix the cache key", level: 1 })).toBeInTheDocument()
    expect(screen.getByText("ci #42 in darkmem on fix/cache-key.")).toBeInTheDocument()
    const summary = within(screen.getByRole("region", { name: "Summary" }))
    expect(summary.getByText("Triggered via pull_request")).toBeInTheDocument()
    expect(summary.getByText("octo-dev")).toBeInTheDocument()
    expect(summary.getByRole("link", { name: "a1f9c3e" })).toHaveAttribute(
      "href",
      "https://github.com/darkraise/darkmem/commit/a1f9c3e0d4b5c6a7b8c9d0e1f2a3b4c5d6e7f8a9",
    )
    expect(summary.getByRole("link", { name: "#87" })).toHaveAttribute("href", "https://github.com/darkraise/darkmem/pull/87")
    expect(summary.getByText("Failed")).toBeInTheDocument()
    expect(summary.getByText("4m10s")).toHaveClass("font-mono")
    expect(summary.getByText("2")).toHaveClass("font-mono")
  })

  it("lists the run's artifacts", async () => {
    mockApi(runRoutes())
    renderApp("/actions/darkmem/runs/102")
    const artifacts = within(await screen.findByRole("region", { name: "Artifacts" }))
    expect(artifacts.getByRole("button", { name: "Download frontend-dist" })).toBeInTheDocument()
    expect(artifacts.getByText("Expired")).toBeInTheDocument()
  })

  it("draws the job graph with tiles linking to the job pages", async () => {
    mockApi(runRoutes())
    renderApp("/actions/darkmem/runs/102")
    const graph = within(await screen.findByRole("region", { name: "ci.yml" }))
    expect(graph.getByRole("link", { name: "Backend tests, Failed, needs Pick runner" })).toHaveAttribute(
      "href",
      "/actions/darkmem/runs/102/jobs/1002?attempt=2",
    )
  })

  it("draws the jobs timeline with the reason when there is no graph", async () => {
    const note = "Add Contents: read to the token to see which jobs depend on which."
    mockApi(runRoutes({ "GET /api/actions/darkmem/runs/102": { ...fixtures.runDetail, graph: undefined, graph_note: note } }))
    renderApp("/actions/darkmem/runs/102")
    const timeline = within(await screen.findByRole("region", { name: "Jobs timeline" }))
    expect(timeline.getByText(note)).toBeInTheDocument()
    expect(timeline.getByRole("link", { name: /Backend tests/ })).toHaveAttribute("href", "/actions/darkmem/runs/102/jobs/1002?attempt=2")
    expect(screen.queryByRole("group", { name: "Job graph" })).toBeNull()
  })

  it("links each annotation to its line in the job's log", async () => {
    mockApi(runRoutes())
    renderApp("/actions/darkmem/runs/102")
    const annotations = within(await screen.findByRole("region", { name: "Annotations" }))
    expect(await annotations.findByRole("link", { name: "Log line 5, Backend tests / Lint (ruff)" })).toHaveAttribute(
      "href",
      "/actions/darkmem/runs/102/jobs/1002?attempt=2&step=3&line=5",
    )
  })

  it("links the rail to the summary and to each job of the shown attempt", async () => {
    mockApi(runRoutes())
    renderApp("/actions/darkmem/runs/102")
    const rail = within(await screen.findByRole("navigation", { name: "Run" }))
    expect(rail.getByRole("link", { name: "Summary" })).toHaveAttribute("aria-current", "page")
    expect(rail.getByRole("link", { name: /Backend tests/ })).toHaveAttribute("href", "/actions/darkmem/runs/102/jobs/1002?attempt=2")
    expect(rail.getByRole("link", { name: /Backend tests/ })).not.toHaveAttribute("aria-current")
  })

  it("asks for the picked attempt through the URL", async () => {
    const { calls } = mockApi(runRoutes())
    const { user, router } = renderApp("/actions/darkmem/runs/102")
    await screen.findByRole("heading", { name: "Fix the cache key", level: 1 })
    await user.click(screen.getByRole("combobox", { name: "Attempt" }))
    await user.click(await screen.findByRole("option", { name: "Attempt 1" }))
    await waitFor(() => expect(router.state.location.search).toEqual({ attempt: 1 }))
    await waitFor(() => expect(runCalls(calls).some((c) => c.path === "/api/actions/darkmem/runs/102" && c.search === "?attempt=1")).toBe(true))
  })

  it("opens the repository's runs from the breadcrumb", async () => {
    mockApi(runRoutes())
    const { user, router } = renderApp("/actions/darkmem/runs/102")
    await screen.findByRole("heading", { name: "Fix the cache key", level: 1 })
    const crumb = within(screen.getByRole("navigation", { name: /breadcrumb/i })).getByRole("link", { name: "darkmem" })
    expect(crumb).toHaveAttribute("href", "/actions?repo=darkmem")
    const build = vi.spyOn(router, "buildLocation")
    await user.click(crumb)
    await waitFor(() => expect(router.state.location.pathname).toBe("/actions"))
    expect(router.state.location.search).toEqual({ repo: "darkmem" })
    expect(build).toHaveBeenCalledWith(expect.objectContaining({ to: "/actions", search: { repo: "darkmem" } }))
  })

  it.each([
    ["a run ID that is not a number", "/actions/darkmem/runs/abc"],
    ["an attempt of zero", "/actions/darkmem/runs/102?attempt=0"],
    ["a job ID that is not a number", "/actions/darkmem/runs/102/jobs/x"],
    ["a line that is not a number", "/actions/darkmem/runs/102/jobs/1002?line=top"],
  ])("shows the not-found line for %s without asking the API", async (_, path) => {
    const { calls } = mockApi(runRoutes())
    renderApp(path)
    expect(await screen.findByText("This run no longer exists on GitHub.")).toBeInTheDocument()
    expect(runCalls(calls)).toEqual([])
  })

  it("names a repository ghr does not cover without asking the API", async () => {
    const { calls } = mockApi(runRoutes())
    renderApp("/actions/other/runs/102")
    expect(await screen.findByText("other is not configured or watched in ghr.")).toBeInTheDocument()
    expect(screen.getByRole("link", { name: "Open Repositories" })).toHaveAttribute("href", "/repositories")
    expect(runCalls(calls)).toEqual([])
  })

  it("reads a watched repository's run", async () => {
    mockApi(runRoutes({ "GET /api/actions/docs/runs/300": { ...fixtures.runDetail, repo: "docs", id: 300, watched: true } }))
    renderApp("/actions/docs/runs/300")
    expect(await screen.findByRole("heading", { name: "Fix the cache key", level: 1 })).toBeInTheDocument()
  })

  it("says the run is gone when GitHub no longer has it", async () => {
    mockApi(runRoutes({ "GET /api/actions/darkmem/runs/102": () => json({ error: "not found on GitHub" }, 404) }))
    renderApp("/actions/darkmem/runs/102")
    expect(await screen.findByText("This run no longer exists on GitHub.")).toBeInTheDocument()
  })

  it("shows the daemon's wording for a missing permission", async () => {
    mockApi(
      runRoutes({
        "GET /api/actions/darkmem/runs/102": () =>
          json({ error: "token lacks actions=read on darkraise/darkmem: grant it in the PAT settings" }, 403),
      }),
    )
    renderApp("/actions/darkmem/runs/102")
    expect(await screen.findByText("token lacks actions=read on darkraise/darkmem: grant it in the PAT settings")).toBeInTheDocument()
  })

  it("lays the summary out as four pairs from 800px", async () => {
    mockApi(runRoutes())
    renderApp("/actions/darkmem/runs/102")
    const status = await screen.findByText("Total duration")
    const list = status.closest("dl") as HTMLElement
    expect(list).toHaveClass("grid-cols-2", "min-[800px]:grid-cols-4")
    expect(list).not.toHaveClass("sm:grid-cols-4")
  })
})

describe("Job page", () => {
  it("keeps Raw log and shows the job's name in the log panel", async () => {
    mockApi(runRoutes())
    renderApp("/actions/darkmem/runs/102/jobs/1002")
    const search = await screen.findByLabelText("Search logs")
    const panel = search.closest(".ghr-log-panel") as HTMLElement
    expect(within(panel).getByRole("button", { name: "Raw log" })).toBeInTheDocument()
    const job = fixtures.runDetail.jobs.find((j) => j.id === 1002)
    expect(within(panel).getByText(job?.name ?? "")).toBeInTheDocument()
  })

  it("shows the job's heading, runner, labels and steps", async () => {
    mockApi(runRoutes())
    renderApp("/actions/darkmem/runs/102/jobs/1002")
    const heading = await screen.findByRole("heading", { name: "Backend tests", level: 2 })
    const job = within(heading.closest("section") as HTMLElement)
    expect(within(heading.parentElement as HTMLElement).getByText("2m15s")).toHaveClass("font-mono")
    expect(job.getByText("ghr-darkmem-d4e5f6")).toHaveClass("font-mono")
    expect(job.getByText("homelab")).toBeInTheDocument()
    expect(job.getByText("ghr")).toHaveClass("font-mono")
    expect(job.getByText("2026-10-03 13:55:25")).toHaveClass("font-mono")
    expect((await job.findAllByText("Lint (ruff)")).length).toBeGreaterThan(0)
    const rail = within(screen.getByRole("navigation", { name: "Run" }))
    expect(rail.getByRole("link", { name: /Backend tests/ })).toHaveAttribute("aria-current", "page")
    expect(rail.getByRole("link", { name: "Summary" })).not.toHaveAttribute("aria-current")
  })

  it("shows a finished job's log with search and the raw log button", async () => {
    const { calls } = mockApi(runRoutes())
    renderApp("/actions/darkmem/runs/102/jobs/1002")
    expect(await screen.findByLabelText("Search logs")).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Raw log" })).toBeInTheDocument()
    expect(calls.some((c) => c.path === "/api/actions/darkmem/jobs/1002/log")).toBe(true)
  })

  it("opens the step and marks the line an annotation links to", async () => {
    mockApi(runRoutes())
    renderApp("/actions/darkmem/runs/102/jobs/1002?attempt=2&step=3&line=5")
    await screen.findByLabelText("Search logs")
    await waitFor(() => expect(document.querySelector('[data-target="true"]')).toHaveTextContent("src/cache.py:12:5: F401 'os' imported but unused"))
  })

  it("marks the new line when an annotation link changes step and line", async () => {
    mockApi(runRoutes())
    const { router } = renderApp("/actions/darkmem/runs/102/jobs/1002?attempt=2&step=3&line=5")
    await waitFor(() => expect(document.querySelector('[data-target="true"]')).toHaveTextContent("src/cache.py:12:5"))
    await router.navigate({
      to: "/actions/$repo/runs/$id/jobs/$jobId",
      params: { repo: "darkmem", id: "102", jobId: "1002" },
      search: { attempt: 2, step: 2, line: 2 },
    })
    await waitFor(() => expect(document.querySelector('[data-target="true"]')).toHaveTextContent("Successfully installed ruff-0.6.9"))
  })

  it("renders no log line again on a clock tick or on a refetch that brings equal data", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true, now: new Date("2026-10-03T14:05:00Z") })
    const run = {
      ...fixtures.runDetail,
      status: "in_progress",
      conclusion: "",
      jobs: fixtures.runDetail.jobs.map((j) => (j.id === 1005 ? { ...j, status: "in_progress", conclusion: "", completed_at: undefined } : j)),
    }
    const { calls } = mockApi(runRoutes({ "GET /api/status": daemonClock, "GET /api/actions/darkmem/runs/102": run }))
    const start = vi.mocked(renderAnsi).mock.calls.length
    const { queryClient } = renderApp("/actions/darkmem/runs/102/jobs/1002")
    await screen.findByLabelText("Search logs")
    const rail = within(screen.getByRole("navigation", { name: "Run" }))
    const runningFor = () => rail.getByRole("link", { name: /E2E/ }).textContent
    const draws = vi.mocked(renderAnsi)
    const drawn = draws.mock.calls.length
    expect(drawn).toBeGreaterThan(start)

    const before = runningFor()
    await act(() => vi.advanceTimersByTimeAsync(2000))
    expect(runningFor()).not.toBe(before)
    expect(draws.mock.calls.length).toBe(drawn)

    const reads = (path: string) => calls.filter((c) => c.path === path).length
    const runReads = reads("/api/actions/darkmem/runs/102")
    const logReads = reads("/api/actions/darkmem/jobs/1002/log")
    // The query library tells its observers on a zero-delay timer.
    await act(async () => {
      await queryClient.refetchQueries()
      await vi.advanceTimersByTimeAsync(50)
    })
    expect(reads("/api/actions/darkmem/runs/102")).toBe(runReads + 1)
    expect(reads("/api/actions/darkmem/jobs/1002/log")).toBe(logReads + 1)
    expect(draws.mock.calls.length).toBe(drawn)
  })

  it("shows why the log could not be read and reads it again on Try again", async () => {
    let busy = true
    mockApi(
      runRoutes({
        "GET /api/actions/darkmem/jobs/1002/log": () =>
          busy ? json({ error: "GitHub is still sending this log; try again shortly" }, 503) : fixtures.jobLog,
      }),
    )
    const { user } = renderApp("/actions/darkmem/runs/102/jobs/1002")
    expect(await screen.findByText("GitHub is still sending this log; try again shortly")).toBeInTheDocument()
    busy = false
    await user.click(screen.getByRole("button", { name: "Try again" }))
    expect(await screen.findByLabelText("Search logs")).toBeInTheDocument()
  })

  it("offers only the raw log for a log too large to show", async () => {
    mockApi(runRoutes({ "GET /api/actions/darkmem/jobs/1002/log": { ...fixtures.jobLog, too_large: true, steps: [] } }))
    renderApp("/actions/darkmem/runs/102/jobs/1002")
    expect(await screen.findByText("This log is too large to show here.")).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Raw log" })).toBeInTheDocument()
    expect(screen.queryByLabelText("Search logs")).toBeNull()
  })

  it("says when GitHub no longer keeps the log", async () => {
    mockApi(runRoutes({ "GET /api/actions/darkmem/jobs/1002/log": { ...fixtures.jobLog, expired: true, steps: [] } }))
    renderApp("/actions/darkmem/runs/102/jobs/1002")
    expect(await screen.findByText("GitHub no longer keeps this job's log.")).toBeInTheDocument()
    expect(screen.queryByRole("button", { name: "Raw log" })).toBeNull()
  })

  it("says when the job produced no log and offers no raw log", async () => {
    mockApi(runRoutes({ "GET /api/actions/darkmem/jobs/1002/log": { ...fixtures.jobLog, no_log: true, steps: [] } }))
    renderApp("/actions/darkmem/runs/102/jobs/1002")
    expect(await screen.findByText("This job produced no log.")).toBeInTheDocument()
    expect(screen.queryByRole("button", { name: "Raw log" })).toBeNull()
    expect(screen.queryByLabelText("Search logs")).toBeNull()
  })

  it("reads the log again on Check again", async () => {
    let stored = false
    const { calls } = mockApi(
      runRoutes({ "GET /api/actions/darkmem/jobs/1002/log": () => (stored ? fixtures.jobLog : { ...fixtures.jobLog, no_log: true, steps: [] }) }),
    )
    const { user } = renderApp("/actions/darkmem/runs/102/jobs/1002")
    expect(await screen.findByText("This job produced no log.")).toBeInTheDocument()
    stored = true
    await user.click(screen.getByRole("button", { name: "Check again" }))
    expect(await screen.findByLabelText("Search logs")).toBeInTheDocument()
    expect(calls.filter((c) => c.path.endsWith("/log"))).toHaveLength(2)
  })

  it("asks again for the log of a job that finished moments ago", async () => {
    vi.useFakeTimers({ toFake: ["setInterval", "clearInterval"] })
    try {
      const recent = {
        ...fixtures.runDetail,
        jobs: fixtures.runDetail.jobs.map((j) => (j.id === 1002 ? { ...j, completed_at: new Date(Date.parse(fixtures.status.now) - 30_000).toISOString() } : j)),
      }
      let stored = false
      const { calls } = mockApi(
        runRoutes({
          "GET /api/actions/darkmem/runs/102": recent,
          "GET /api/actions/darkmem/jobs/1002/log": () => (stored ? fixtures.jobLog : { ...fixtures.jobLog, no_log: true, steps: [] }),
        }),
      )
      renderApp("/actions/darkmem/runs/102/jobs/1002")
      expect(await screen.findByText("GitHub has not stored this job's log yet; checking again.")).toBeInTheDocument()
      expect(screen.getByRole("button", { name: "Check again" })).toBeInTheDocument()
      stored = true
      await act(() => vi.advanceTimersByTimeAsync(15_000))
      expect(await screen.findByLabelText("Search logs")).toBeInTheDocument()
      expect(calls.filter((c) => c.path.endsWith("/log"))).toHaveLength(2)
    } finally {
      vi.useRealTimers()
    }
  })

  it("does not wait for a log of a job that completed after the page's clock", async () => {
    const skewed = {
      ...fixtures.runDetail,
      jobs: fixtures.runDetail.jobs.map((j) => (j.id === 1002 ? { ...j, completed_at: new Date(Date.parse(fixtures.status.now) + 10 * 60_000).toISOString() } : j)),
    }
    mockApi(
      runRoutes({
        "GET /api/actions/darkmem/runs/102": skewed,
        "GET /api/actions/darkmem/jobs/1002/log": { ...fixtures.jobLog, no_log: true, steps: [] },
      }),
    )
    renderApp("/actions/darkmem/runs/102/jobs/1002")
    expect(await screen.findByText("This job produced no log.")).toBeInTheDocument()
  })

  it("saves the raw log through a Blob", async () => {
    const text = "2026-10-03T13:55:25.0000000Z Set up job\n"
    const { calls, fetchMock } = mockApi(
      runRoutes({
        "GET /api/actions/darkmem/jobs/1002/log/raw": () => new Response(text, { headers: { "Content-Type": "text/plain; charset=utf-8" } }),
      }),
    )
    const blobs: Blob[] = []
    const create = vi.fn((obj: Blob | MediaSource) => {
      blobs.push(obj as Blob)
      return "blob:http://localhost/raw"
    })
    const revoke = vi.fn()
    // jsdom has no object URLs; a subclass keeps new URL(...) working.
    class FakeURL extends URL {
      static createObjectURL = create
      static revokeObjectURL = revoke
    }
    vi.stubGlobal("URL", FakeURL)
    const saved: string[] = []
    const click = vi.spyOn(HTMLAnchorElement.prototype, "click").mockImplementation(function (this: HTMLAnchorElement) {
      saved.push(`${this.download} ${this.href}`)
    })
    const { user } = renderApp("/actions/darkmem/runs/102/jobs/1002")
    await user.click(await screen.findByRole("button", { name: "Raw log" }))
    await waitFor(() => expect(saved).toEqual(["darkmem-job-1002.log blob:http://localhost/raw"]))
    expect(blobs).toHaveLength(1)
    expect(await blobs.at(0)?.text()).toBe(text)
    expect(calls.some((c) => c.path === "/api/actions/darkmem/jobs/1002/log/raw")).toBe(true)
    const rawCall = fetchMock.mock.calls.find(([input]) => typeof input === "string" && input.endsWith("/log/raw"))
    expect(new Headers(rawCall?.[1]?.headers).get("X-GHR")).toBe("1")
    expect(rawCall?.[1]?.credentials).toBe("same-origin")
    await waitFor(() => expect(revoke).toHaveBeenCalledWith("blob:http://localhost/raw"))
    click.mockRestore()
  })

  it.each([
    ["an error answer", () => json({ error: "log is larger than 32 MiB; open it on GitHub" }, 413), "log is larger than 32 MiB; open it on GitHub"],
    [
      "a download that breaks",
      () =>
        new Response(
          new ReadableStream({
            start: (c) => {
              c.enqueue(new TextEncoder().encode("part"))
              c.error(new Error("connection dropped"))
            },
          }),
        ),
      "connection dropped",
    ],
  ])("toasts a raw log save that fails: %s", async (_name, route, message) => {
    mockApi(runRoutes({ "GET /api/actions/darkmem/jobs/1002/log/raw": route }))
    const { user } = renderApp("/actions/darkmem/runs/102/jobs/1002")
    await user.click(await screen.findByRole("button", { name: "Raw log" }))
    expect((await screen.findAllByText(message)).length).toBeGreaterThan(0)
  })

  it("says the job is gone when GitHub has no log for it", async () => {
    mockApi(runRoutes({ "GET /api/actions/darkmem/jobs/1002/log": () => json({ error: "not found on GitHub" }, 404) }))
    renderApp("/actions/darkmem/runs/102/jobs/1002")
    expect(await screen.findByText("This run no longer exists on GitHub.")).toBeInTheDocument()
  })

  it("says logs wait for a running job and lists its steps", async () => {
    const running = {
      ...fixtures.runDetail,
      status: "in_progress",
      conclusion: "",
      jobs: fixtures.runDetail.jobs.map((j) => (j.id === 1002 ? { ...j, status: "in_progress", conclusion: "", completed_at: undefined } : j)),
    }
    const { calls } = mockApi(runRoutes({ "GET /api/actions/darkmem/runs/102": running }))
    renderApp("/actions/darkmem/runs/102/jobs/1002")
    expect(await screen.findByText("Logs appear when the job finishes.")).toBeInTheDocument()
    expect(screen.getByRole("list", { name: "Steps" })).toBeInTheDocument()
    expect(calls.some((c) => c.path.endsWith("/log"))).toBe(false)
  })

  it("says when the job is not part of the shown attempt", async () => {
    mockApi(runRoutes())
    renderApp("/actions/darkmem/runs/102/jobs/999")
    expect(await screen.findByText("This job is not part of attempt 2.")).toBeInTheDocument()
    expect(screen.getByRole("link", { name: "Open the run" })).toHaveAttribute("href", "/actions/darkmem/runs/102?attempt=2")
  })
})
