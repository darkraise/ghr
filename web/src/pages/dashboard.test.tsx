import { screen, waitFor, within } from "@testing-library/react"
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
    expect(await screen.findByText("no repos configured")).toBeInTheDocument()
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

  it("switches the mode and steps the global max", async () => {
    const { calls } = mockApi(authedRoutes({ "PATCH /api/config": () => noContent() }))
    const { user } = renderApp("/")
    await user.click(await screen.findByRole("button", { name: "Switch to ALL" }))
    await waitFor(() => expect(calls.some((c) => c.method === "PATCH" && JSON.stringify(c.body) === '{"mode":"all"}')).toBe(true))
    await waitFor(() => expect(screen.getByRole("button", { name: "Raise global max" })).toBeEnabled())
    await user.click(screen.getByRole("button", { name: "Raise global max" }))
    await waitFor(() => expect(calls.some((c) => c.method === "PATCH" && JSON.stringify(c.body) === '{"global_max":3}')).toBe(true))
    expect((await screen.findAllByText("global max 3")).length).toBeGreaterThan(0)
  })

  it("lowers the global max no further than 1", async () => {
    mockApi(authedRoutes({ "GET /api/status": { ...fixtures.status, global_max: 1 } }))
    renderApp("/")
    expect(await screen.findByRole("button", { name: "Lower global max" })).toBeDisabled()
  })

  it("steps a repo's max, except an unlimited one", async () => {
    const repos = fixtures.status.repos.map((r) => (r.name === "darkcloud" ? { ...r, max: 0 } : r))
    const { calls } = mockApi(authedRoutes({ "GET /api/status": { ...fixtures.status, repos }, "PATCH /api/config": () => noContent() }))
    const { user } = renderApp("/")
    expect(await screen.findByRole("button", { name: "Raise max for darkcloud" })).toBeDisabled()
    expect(screen.getByRole("button", { name: "Lower max for darkcloud" })).toBeDisabled()
    expect(screen.getByRole("button", { name: "Lower max for old-repo" })).toBeDisabled()
    await user.click(screen.getByRole("button", { name: "Raise max for darkmem" }))
    await waitFor(() =>
      expect(calls.some((c) => c.method === "PATCH" && JSON.stringify(c.body) === '{"repos":{"darkmem":{"max":3}}}')).toBe(true),
    )
  })

  it("adds, edits, pauses and removes repos, and acts on runners", async () => {
    const { calls } = mockApi(
      authedRoutes({
        "GET /api/repos/available": fixtures.availableRepos,
        "POST /api/repos/darkmem/pause": () => noContent(),
        "DELETE /api/repos/darkmem": () => noContent(),
      }),
    )
    const { user } = renderApp("/")
    expect(await screen.findByRole("link", { name: "Edit darkmem" })).toHaveAttribute("href", "/repositories/darkmem")
    expect(screen.getByRole("button", { name: "Stop runner aaaaaa" })).toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: "Pause darkmem" }))
    await waitFor(() => expect(calls.some((c) => c.method === "POST" && c.path === "/api/repos/darkmem/pause")).toBe(true))
    await user.click(screen.getByRole("button", { name: "Remove darkmem" }))
    const ask = within(await screen.findByRole("alertdialog"))
    expect(calls.some((c) => c.method === "DELETE")).toBe(false)
    await user.click(ask.getByRole("button", { name: "Remove" }))
    await waitFor(() => expect(calls.some((c) => c.method === "DELETE" && c.path === "/api/repos/darkmem")).toBe(true))
    await user.click(screen.getByRole("button", { name: "+ Add repository" }))
    expect(await screen.findByRole("heading", { name: "Add repository" })).toBeInTheDocument()
  })
})
