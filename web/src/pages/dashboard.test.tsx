import { screen, waitFor } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import { json, mockApi, noContent } from "@/test/api"
import { authedRoutes, fixtures } from "@/test/fixtures"
import { renderApp } from "@/test/render"

describe("Dashboard page", () => {
  it("shows the status chips", async () => {
    mockApi(authedRoutes())
    renderApp("/")
    expect(await screen.findByText("mode QUEUE")).toBeInTheDocument()
    expect(screen.getByText("runners 2/2")).toBeInTheDocument()
    expect(screen.getByText("api 4980")).toBeInTheDocument()
    expect(screen.getByText("disk 61%")).toBeInTheDocument()
    expect(screen.getByText("runner ↑ 2.338.0")).toBeInTheDocument()
  })

  it("shows the metric tiles", async () => {
    mockApi(authedRoutes())
    renderApp("/")
    expect(await screen.findByText("2 / 2")).toBeInTheDocument()
    expect(await screen.findByText("31%")).toBeInTheDocument()
    expect(screen.getByText("3.0G / 16.0G")).toBeInTheDocument()
    expect(screen.getByRole("img", { name: "running" })).toBeInTheDocument()
  })

  it("shows repos, runners and the activity feed", async () => {
    mockApi(authedRoutes())
    renderApp("/")
    expect(await screen.findByText("old-repo")).toBeInTheDocument()
    expect(screen.getByText("GitHub: not found")).toBeInTheDocument()
    expect(screen.getByText("paused")).toBeInTheDocument()
    expect(screen.getByText(/#41 build/)).toBeInTheDocument()
    expect(screen.getByRole("link", { name: "aaaaaa" })).toBeInTheDocument()
    expect(await screen.findByText("runner aaaaaa started")).toBeInTheDocument()
  })

  it("pauses every repo", async () => {
    const { calls } = mockApi(authedRoutes({ "POST /api/pause-all": () => noContent() }))
    const { user } = renderApp("/")
    await user.click(await screen.findByRole("button", { name: "Pause all" }))
    await waitFor(() => expect(calls.some((c) => c.method === "POST" && c.path === "/api/pause-all")).toBe(true))
    expect((await screen.findAllByText("paused all repos (drain)")).length).toBeGreaterThan(0)
  })

  it("resumes when every live repo is paused", async () => {
    const repos = fixtures.status.repos.map((r) => ({ ...r, paused: true }))
    const { calls } = mockApi(
      authedRoutes({ "GET /api/status": { ...fixtures.status, repos }, "POST /api/resume-all": () => noContent() }),
    )
    const { user } = renderApp("/")
    await user.click(await screen.findByRole("button", { name: "Resume all" }))
    await waitFor(() => expect(calls.some((c) => c.path === "/api/resume-all")).toBe(true))
  })

  it("shows a metrics failure in the tiles", async () => {
    mockApi(authedRoutes({ "GET /api/metrics": () => json({ error: "boom" }, 500) }))
    renderApp("/")
    expect((await screen.findAllByText("✖ boom")).length).toBe(2)
  })

  it("shows the empty states", async () => {
    mockApi(authedRoutes({ "GET /api/status": { ...fixtures.status, repos: [], instances: [] }, "GET /api/events": [] }))
    renderApp("/")
    expect(await screen.findByText("no repos configured — add one on the Repositories page")).toBeInTheDocument()
    expect(screen.getByText("no runners — they start when jobs are queued")).toBeInTheDocument()
    expect(screen.getByText("no activity yet")).toBeInTheDocument()
  })

  it("waits for the daemon while it cannot be reached", async () => {
    mockApi(authedRoutes({ "GET /api/status": () => json({ error: "connection refused" }, 502) }))
    renderApp("/")
    expect(await screen.findByText("waiting for the daemon…")).toBeInTheDocument()
  })

  it("names the status glyphs", async () => {
    mockApi(authedRoutes())
    renderApp("/")
    expect(await screen.findByRole("img", { name: "succeeded" })).toHaveTextContent("✔")
    expect(screen.getByRole("img", { name: "queued" })).toHaveTextContent("⧗")
    expect((await screen.findAllByRole("img", { name: "warning" }))[0]).toHaveTextContent("⚠")
  })

  it("marks a cancelled last job with its own icon", async () => {
    const repos = fixtures.status.repos.map((r) => (r.last_job ? { ...r, last_job: { ...r.last_job, conclusion: "cancelled" } } : r))
    mockApi(authedRoutes({ "GET /api/status": { ...fixtures.status, repos } }))
    renderApp("/")
    expect(await screen.findByRole("img", { name: "cancelled" })).toHaveTextContent("⊘")
  })
})
