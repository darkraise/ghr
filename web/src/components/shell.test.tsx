import { screen, waitFor } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"
import { keys } from "@/api/hooks"
import { json, mockApi, noContent } from "@/test/api"
import { authedRoutes, fixtures } from "@/test/fixtures"
import { renderApp } from "@/test/render"

describe("shell", () => {
  it("lists the seven pages in the sidebar", async () => {
    mockApi(authedRoutes())
    renderApp("/")
    for (const label of ["Dashboard", "Repositories", "Runners", "History", "Toolchains", "Storage", "Settings"]) {
      expect((await screen.findAllByRole("link", { name: label })).length).toBeGreaterThan(0)
    }
  })

  it("shows reconnecting while status polls fail, until one succeeds", async () => {
    let fail = true
    mockApi(authedRoutes({ "GET /api/status": () => (fail ? json({ error: "connection refused" }, 502) : fixtures.status) }))
    renderApp("/")
    expect(await screen.findByText("daemon unreachable: connection refused — retrying")).toBeInTheDocument()
    fail = false
    await waitFor(() => expect(screen.queryByText(/daemon unreachable/)).toBeNull(), { timeout: 3000 })
  })

  it("shows why the daemon is degraded", async () => {
    mockApi(authedRoutes({ "GET /api/status": { ...fixtures.status, degraded: true, degraded_reason: "GitHub rejected the token" } }))
    renderApp("/")
    expect(await screen.findByText("DEGRADED: GitHub rejected the token — no new runners")).toBeInTheDocument()
  })

  it("refetches the config when the daemon restarts", async () => {
    let epoch = "e1"
    mockApi(authedRoutes({ "GET /api/status": () => ({ ...fixtures.status, epoch }) }))
    const { queryClient } = renderApp("/")
    const invalidate = vi.spyOn(queryClient, "invalidateQueries")
    await waitFor(() => expect(queryClient.getQueryData(keys.status)).toBeDefined())
    expect(invalidate).not.toHaveBeenCalledWith({ queryKey: keys.config })
    epoch = "e2"
    await waitFor(() => expect(invalidate).toHaveBeenCalledWith({ queryKey: keys.config }), { timeout: 3000 })
  })

  it("keeps config, metrics and events polling on every page", async () => {
    const { calls } = mockApi(authedRoutes())
    renderApp("/storage")
    await screen.findByRole("heading", { name: "Storage" })
    await waitFor(() => {
      for (const path of ["/api/config", "/api/metrics", "/api/events"]) {
        expect(calls.some((c) => c.path === path)).toBe(true)
      }
    })
  })

  it("logs out from the user menu", async () => {
    let authenticated = true
    const { calls } = mockApi(
      authedRoutes({
        "GET /auth/state": () => ({ setup_required: false, authenticated }),
        "POST /auth/logout": () => {
          authenticated = false
          return noContent()
        },
      }),
    )
    const { user, router } = renderApp("/")
    await user.click(await screen.findByRole("button", { name: "O" }))
    await user.click(await screen.findByText("Log out"))
    await waitFor(() => expect(router.state.location.pathname).toBe("/login"))
    expect(calls.some((c) => c.method === "POST" && c.path === "/auth/logout")).toBe(true)
  })

  it("lands on /login from the user menu even when the logout request fails", async () => {
    let authenticated = true
    const { calls } = mockApi(
      authedRoutes({
        "GET /auth/state": () => ({ setup_required: false, authenticated }),
        "POST /auth/logout": () => {
          authenticated = false
          return json({ error: "session already ended" }, 500)
        },
      }),
    )
    const { user, router } = renderApp("/")
    await user.click(await screen.findByRole("button", { name: "O" }))
    await user.click(await screen.findByText("Log out"))
    await waitFor(() => expect(router.state.location.pathname).toBe("/login"))
    expect(calls.some((c) => c.method === "POST" && c.path === "/auth/logout")).toBe(true)
  })

  it("logs out from the user menu past unsaved edits", async () => {
    let authenticated = true
    mockApi(
      authedRoutes({
        "GET /auth/state": () => ({ setup_required: false, authenticated }),
        "POST /auth/logout": () => {
          authenticated = false
          return noContent()
        },
      }),
    )
    const { user, router } = renderApp("/settings")
    const poll = await screen.findByLabelText("Poll interval")
    await user.clear(poll)
    await user.type(poll, "15s")
    await user.click(screen.getByRole("button", { name: "O" }))
    await user.click(await screen.findByRole("menuitem", { name: "Log out" }))
    await waitFor(() => expect(router.state.location.pathname).toBe("/login"))
    expect(screen.queryByText("Unsaved changes")).toBeNull()
  })

  it("sends an expired session to /login", async () => {
    let authenticated = true
    mockApi(
      authedRoutes({
        "GET /auth/state": () => ({ setup_required: false, authenticated }),
        "GET /api/status": () => {
          authenticated = false
          return json({ error: "not logged in" }, 401)
        },
      }),
    )
    const { router } = renderApp("/")
    await waitFor(() => expect(router.state.location.pathname).toBe("/login"), { timeout: 3000 })
    expect(screen.queryByText(/daemon unreachable/)).toBeNull()
  })

  it("shows the status chips on every page", async () => {
    mockApi(authedRoutes({ "GET /api/history": fixtures.history }))
    renderApp("/history")
    expect(await screen.findByText("mode QUEUE")).toBeInTheDocument()
    expect(screen.getByText("runners 2/2")).toBeInTheDocument()
    expect(screen.getByText("api 4980")).toBeInTheDocument()
    expect(screen.getByText("disk 61%")).toBeInTheDocument()
    expect(screen.getByText("runner ↑ 2.338.0")).toBeInTheDocument()
  })
})
