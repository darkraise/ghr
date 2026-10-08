import { screen, waitFor, within } from "@testing-library/react"
import { beforeEach, describe, expect, it } from "vitest"
import { json, mockApi, noContent } from "@/test/api"
import { authedRoutes, fixtures } from "@/test/fixtures"
import { renderApp } from "@/test/render"

const region = (name: string) => within(screen.getByRole("region", { name }))

describe("Dashboard page", () => {
  beforeEach(() => localStorage.clear())

  it("sums up the daemon in one sentence", async () => {
    mockApi(authedRoutes())
    renderApp("/")
    expect(await screen.findByText("1 of 2 runners busy. 3 jobs are waiting in darkmem. old-repo: GitHub: not found.")).toBeInTheDocument()
  })

  it("shows the stat cards", async () => {
    mockApi(authedRoutes())
    renderApp("/")
    await screen.findByRole("region", { name: "Runners" })
    expect(region("Runners").getByText("of 2 busy")).toBeInTheDocument()
    expect(region("Waiting jobs").getByText("oldest 3 min")).toBeInTheDocument()
    expect(region("API budget").getByText("4,980")).toBeInTheDocument()
    await waitFor(() => expect(region("Host").getByText("31%")).toBeInTheDocument())
  })

  it("asks for the remembered activity window in the browser's zone", async () => {
    localStorage.setItem("ghr-activity-window", "24h")
    const { calls } = mockApi(authedRoutes())
    renderApp("/")
    expect(await screen.findByRole("group", { name: "Activity per bucket for the last 24 hours" })).toBeInTheDocument()
    expect(calls.find((c) => c.path === "/api/activity")?.search).toBe("?window=24h&tz=UTC")
  })

  it("switches the activity window and remembers it", async () => {
    const { calls } = mockApi(authedRoutes())
    const { user } = renderApp("/")
    await screen.findByRole("group", { name: "Runner lanes for the last hour" })
    await user.click(screen.getByRole("radio", { name: "7d" }))
    await waitFor(() => expect(calls.some((c) => c.search === "?window=7d&tz=UTC")).toBe(true))
    expect(localStorage.getItem("ghr-activity-window")).toBe("7d")
  })

  it("opens a runner from its running bar", async () => {
    mockApi(authedRoutes())
    const { user, router } = renderApp("/")
    await user.click(await screen.findByRole("link", { name: /^darkmem, ci, test, run 42, running/ }))
    await waitFor(() => expect(router.state.location.pathname).toBe("/runners/aaaaaa"))
  })

  it("lists repositories and opens one", async () => {
    mockApi(authedRoutes())
    const { user, router } = renderApp("/")
    await screen.findByRole("region", { name: "Repositories" })
    expect(region("Repositories").getByText("Running")).toBeInTheDocument()
    expect(region("Repositories").getByText("2 configured")).toBeInTheDocument()
    expect(region("Repositories").getByRole("link", { name: "Manage" })).toHaveAttribute("href", "/repositories")
    await user.click(region("Repositories").getByRole("link", { name: "darkcloud" }))
    await waitFor(() => expect(router.state.location.pathname).toBe("/repositories/darkcloud"))
  })

  it("pauses every repository", async () => {
    const { calls } = mockApi(authedRoutes({ "POST /api/pause-all": () => noContent() }))
    const { user } = renderApp("/")
    await user.click(await screen.findByRole("button", { name: "Pause all" }))
    await waitFor(() => expect(calls.some((c) => c.method === "POST" && c.path === "/api/pause-all")).toBe(true))
    expect((await screen.findAllByText("Paused all repositories. Running jobs finish first.")).length).toBeGreaterThan(0)
  })

  it("resumes when every live repository is paused", async () => {
    const repos = fixtures.status.repos.map((r) => ({ ...r, paused: true }))
    const { calls } = mockApi(authedRoutes({ "GET /api/status": { ...fixtures.status, repos }, "POST /api/resume-all": () => noContent() }))
    const { user } = renderApp("/")
    await user.click(await screen.findByRole("button", { name: "Resume all" }))
    await waitFor(() => expect(calls.some((c) => c.path === "/api/resume-all")).toBe(true))
  })

  it("shows disk use", async () => {
    mockApi(authedRoutes())
    renderApp("/")
    await screen.findByRole("region", { name: "Disk" })
    await waitFor(() => expect(region("Disk").getByText("146.0 GB of 240.0 GB used, prunes at 80%")).toBeInTheDocument())
    expect(region("Disk").getByRole("link", { name: "Storage" })).toHaveAttribute("href", "/storage")
    await waitFor(() => expect(region("Disk").getByText("Images")).toBeInTheDocument())
  })

  it("shows the latest events", async () => {
    mockApi(authedRoutes())
    renderApp("/")
    expect(await screen.findByText("runner aaaaaa started")).toBeInTheDocument()
  })

  it("shows the empty states", async () => {
    mockApi(authedRoutes({ "GET /api/status": { ...fixtures.status, repos: [], instances: [] }, "GET /api/events": [] }))
    renderApp("/")
    expect(await screen.findByText("No repositories yet")).toBeInTheDocument()
    expect(screen.getByText("No events yet.")).toBeInTheDocument()
    expect(screen.getByRole("heading", { name: "Activity" })).toBeInTheDocument()
  })

  it("waits for the daemon while it cannot be reached", async () => {
    mockApi(authedRoutes({ "GET /api/status": () => json({ error: "connection refused" }, 502) }))
    renderApp("/")
    expect(await screen.findByText("Waiting for the daemon")).toBeInTheDocument()
  })

  it("disables its actions while the daemon is unreachable", async () => {
    let fail = false
    mockApi(authedRoutes({ "GET /api/status": () => (fail ? json({ error: "connection refused" }, 502) : fixtures.status) }))
    renderApp("/")
    for (const name of ["Pause all", "Add repository"]) expect(await screen.findByRole("button", { name })).toBeEnabled()
    fail = true
    await waitFor(() => expect(screen.getByRole("button", { name: "Pause all" })).toBeDisabled(), { timeout: 3000 })
    expect(screen.getByRole("button", { name: "Add repository" })).toBeDisabled()
  })

  it("opens the add repository dialog", async () => {
    mockApi(authedRoutes({ "GET /api/repos/available": fixtures.availableRepos }))
    const { user } = renderApp("/")
    await user.click(await screen.findByRole("button", { name: "Add repository" }))
    expect(await screen.findByRole("heading", { name: "Add repository" })).toBeInTheDocument()
  })
})
