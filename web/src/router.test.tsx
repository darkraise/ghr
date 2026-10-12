import { act, screen, waitFor } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"
import { api } from "@/api/client"
import { json, mockApi, type Call } from "@/test/api"
import { authedRoutes, fixtures } from "@/test/fixtures"
import { renderApp } from "@/test/render"

const authed = { "GET /auth/state": { setup_required: false, authenticated: true } }
const anonymous = { "GET /auth/state": { setup_required: false, authenticated: false } }

function renderAt(path: string) {
  return renderApp(path).router
}

function asked(calls: Call[], path: string): number {
  return calls.filter((c) => c.path === path).length
}

describe("routes", () => {
  afterEach(() => {
    vi.useRealTimers()
  })

  it("sends a visitor without a session to /login", async () => {
    mockApi(anonymous)
    const router = renderAt("/runners")
    expect(await screen.findByRole("button", { name: "Log in" })).toBeInTheDocument()
    expect(router.state.location.pathname).toBe("/login")
  })

  it("sends a logged-in visitor from /login to the dashboard", async () => {
    mockApi(authed)
    const router = renderAt("/login")
    expect(await screen.findByRole("heading", { name: "Dashboard" })).toBeInTheDocument()
    expect(router.state.location.pathname).toBe("/")
  })

  it("sends a logged-in visitor from /login to the page it asked for", async () => {
    mockApi(authedRoutes({ "GET /api/history": [] }))
    const router = renderAt("/login?redirect=%2Fhistory")
    expect(await screen.findByRole("heading", { name: "History" })).toBeInTheDocument()
    expect(router.state.location.pathname).toBe("/history")
  })

  it("keeps a logged-in visitor on the site whatever /login is asked to open", async () => {
    mockApi(authed)
    const router = renderAt("/login?redirect=%2F%2Fevil.example%2Fx")
    expect(await screen.findByRole("heading", { name: "Dashboard" })).toBeInTheDocument()
    expect(router.state.location.pathname).toBe("/")
  })

  it.each([
    ["/runners", "Runners"],
    ["/history", "History"],
    ["/repositories", "Repositories"],
    ["/toolchains", "Toolchains"],
    ["/storage", "Storage"],
    ["/settings", "General"],
    ["/runners/aaaaaa", "aaaaaa"],
  ])("serves %s", async (path, title) => {
    mockApi(authed)
    renderAt(path)
    const query = path === "/settings" ? { level: 1, name: title } : { name: title }
    expect(await screen.findByRole("heading", query)).toBeInTheDocument()
  })

  it("offers a retry when the daemon cannot be asked who is logged in", async () => {
    let down = true
    mockApi({
      "GET /auth/state": () => (down ? json({ error: "connection refused" }, 502) : authed["GET /auth/state"]),
    })
    const { user } = renderApp("/runners")
    expect(await screen.findByText("cannot load the page: connection refused")).toBeInTheDocument()
    down = false
    await user.click(screen.getByRole("button", { name: "Retry" }))
    expect(await screen.findByRole("heading", { name: "Runners" })).toBeInTheDocument()
  })

  it("keeps the page when the auth check fails and the cache holds a session", async () => {
    vi.useFakeTimers({ toFake: ["Date"] })
    let down = false
    const { calls } = mockApi(
      authedRoutes({
        "GET /auth/state": () => (down ? json({ error: "connection refused" }, 502) : authed["GET /auth/state"]),
        "GET /api/history": [],
      }),
    )
    const { user } = renderApp("/runners")
    expect(await screen.findByRole("heading", { name: "Runners" })).toBeInTheDocument()
    down = true
    vi.setSystemTime(Date.now() + 5000)
    await user.click(screen.getByRole("link", { name: "History" }))
    expect(await screen.findByRole("heading", { name: "History" })).toBeInTheDocument()
    expect(screen.queryByText(/cannot load the page/)).toBeNull()
    expect(asked(calls, "/auth/state")).toBe(2)
  })

  it("serves a settings section under /settings", async () => {
    mockApi(authedRoutes())
    const { router } = renderApp("/settings/token")
    expect(await screen.findByRole("heading", { level: 1, name: "GitHub token" })).toBeInTheDocument()
    expect(router.state.location.pathname).toBe("/settings/token")
    expect(router.state.matches.at(-1)?.routeId).toBe("/app/settings/$section")
  })

  it.each([
    ["/settings/nope", "Settings has no such section."],
    ["/repositories/darkmem/nope", "This repository has no such section."],
  ])("says %s is no section", async (path, line) => {
    mockApi(authedRoutes())
    renderApp(path)
    expect(await screen.findByText(line)).toBeInTheDocument()
  })

  it("serves a repository section under its repository", async () => {
    mockApi(authedRoutes())
    const { router } = renderApp("/repositories/darkmem/settings")
    expect(await screen.findByRole("heading", { level: 1, name: "darkmem" })).toBeInTheDocument()
    expect(router.state.matches.at(-1)?.routeId).toBe("/app/repositories/$name/$section")
  })

  it("reads the runner detail tab from the search", async () => {
    mockApi(authed)
    const router = renderAt("/runners/aaaaaa?tab=log")
    await waitFor(() => expect(router.state.location.search).toEqual({ tab: "log" }))
  })

  it("falls back to the steps tab for an unknown tab", async () => {
    mockApi(authed)
    const router = renderAt("/runners/aaaaaa?tab=bogus")
    await waitFor(() => expect(router.state.location.search).toEqual({ tab: "steps" }))
  })

  const setupState = (over: Record<string, unknown>) => ({
    configured: true,
    starting: false,
    setup_pending: false,
    toolchains_pending: false,
    owner: "darkraise",
    web_listen: "0.0.0.0:8080",
    ...over,
  })

  it("sends an unconfigured daemon to /setup", async () => {
    mockApi(authedRoutes({ "GET /api/setup": setupState({ configured: false, owner: "" }) }))
    const router = renderAt("/runners")
    expect(await screen.findByRole("heading", { name: "Set up ghr" })).toBeInTheDocument()
    expect(router.state.location.pathname).toBe("/setup")
  })

  it("sends a finished setup from /setup to the dashboard", async () => {
    mockApi(authedRoutes({ "GET /api/setup": setupState({}) }))
    const router = renderAt("/setup")
    expect(await screen.findByRole("heading", { name: "Dashboard" })).toBeInTheDocument()
    expect(router.state.location.pathname).toBe("/")
  })

  it("shows a banner while setup is pending, linking to /setup", async () => {
    mockApi(
      authedRoutes({
        "GET /api/setup": setupState({ setup_pending: true }),
        "GET /api/status": { ...fixtures.status, setup_pending: true },
      }),
    )
    const view = renderApp("/")
    expect(await screen.findByText("First-run setup is not finished.")).toBeInTheDocument()
    await view.user.click(screen.getByRole("link", { name: "Finish setup" }))
    await waitFor(() => expect(view.router.state.location.pathname).toBe("/setup"))
  })

  it("asks who is logged in and whether setup is done again only after five seconds", async () => {
    vi.useFakeTimers({ toFake: ["Date"] })
    const { calls } = mockApi(authedRoutes({ "GET /api/setup": setupState({}), "GET /api/history": [] }))
    const router = renderAt("/history")
    expect(await screen.findByRole("heading", { name: "History" })).toBeInTheDocument()
    await act(() => router.navigate({ to: "/history", search: { result: "failure" } }))
    expect(router.state.location.search).toEqual({ result: "failure" })
    expect(asked(calls, "/auth/state")).toBe(1)
    expect(asked(calls, "/api/setup")).toBe(1)
    vi.setSystemTime(Date.now() + 5000)
    await act(() => router.navigate({ to: "/history", search: {} }))
    expect(router.state.location.search).toEqual({})
    expect(asked(calls, "/auth/state")).toBe(2)
    expect(asked(calls, "/api/setup")).toBe(2)
  })

  it("shows the login form when a session ends while the auth answer is fresh", async () => {
    let authenticated = true
    mockApi(
      authedRoutes({
        "GET /auth/state": () => ({ setup_required: false, authenticated }),
        "GET /api/status": () => (authenticated ? fixtures.status : json({ error: "not logged in" }, 401)),
      }),
    )
    const router = renderAt("/")
    expect(await screen.findByRole("heading", { name: "Dashboard" })).toBeInTheDocument()
    authenticated = false
    await act(async () => {
      await api.status().catch(() => undefined)
    })
    expect(await screen.findByRole("button", { name: "Log in" })).toBeInTheDocument()
    expect(router.state.location.pathname).toBe("/login")
  })

  it("loads the app when the setup state cannot be read", async () => {
    mockApi(authedRoutes({ "GET /api/history": [] }))
    renderAt("/history")
    expect(await screen.findByRole("heading", { name: "History" })).toBeInTheDocument()
  })

  it.each([
    ["/toolchains", "/settings/toolchains", ""],
    ["/storage", "/settings/storage", ""],
    ["/toolchains#installed", "/settings/toolchains", "installed"],
    ["/storage#prune", "/settings/storage", "prune"],
    ["/settings/runner", "/settings/updates", ""],
    ["/settings/runner#runner", "/settings/updates", "runner"],
  ])("sends %s to its settings section, keeping the hash", async (from, to, hash) => {
    mockApi(authedRoutes())
    const { router } = renderApp(from)
    await waitFor(() => expect(router.state.location.pathname).toBe(to))
    expect(router.state.location.hash).toBe(hash)
  })
})
