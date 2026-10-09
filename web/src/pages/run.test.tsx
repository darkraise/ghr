import { screen, waitFor, within } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import { json, mockApi, type Call } from "@/test/api"
import { fixtures, runRoutes } from "@/test/fixtures"
import { renderApp } from "@/test/render"

const runCalls = (calls: Call[]) => calls.filter((c) => c.path.startsWith("/api/actions/"))

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
})

describe("Job page", () => {
  it("shows the job's heading, runner, labels and steps", async () => {
    mockApi(runRoutes())
    renderApp("/actions/darkmem/runs/102/jobs/1002")
    const heading = await screen.findByRole("heading", { name: "Backend tests", level: 2 })
    const job = within(heading.closest("section") as HTMLElement)
    expect(job.getByText("2m15s")).toHaveClass("font-mono")
    expect(job.getByText("ghr-darkmem-d4e5f6")).toHaveClass("font-mono")
    expect(job.getByText("homelab")).toBeInTheDocument()
    expect(job.getByText("ghr")).toHaveClass("font-mono")
    expect(job.getByText("2026-10-03 13:55:25")).toHaveClass("font-mono")
    expect(job.getAllByText("Lint (ruff)").length).toBeGreaterThan(0)
    const rail = within(screen.getByRole("navigation", { name: "Run" }))
    expect(rail.getByRole("link", { name: /Backend tests/ })).toHaveAttribute("aria-current", "page")
    expect(rail.getByRole("link", { name: "Summary" })).not.toHaveAttribute("aria-current")
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
