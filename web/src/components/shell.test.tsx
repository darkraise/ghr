import { screen, waitFor, within } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"
import { keys } from "@/api/hooks"
import { json, mockApi, noContent } from "@/test/api"
import { authedRoutes, fixtures } from "@/test/fixtures"
import { renderApp } from "@/test/render"

function logoutRoutes(answer: () => Response) {
  let authenticated = true
  return authedRoutes({
    "GET /auth/state": () => ({ setup_required: false, authenticated }),
    "POST /auth/logout": () => {
      authenticated = false
      return answer()
    },
  })
}

describe("shell", () => {
  it("lists the pages in two groups", async () => {
    mockApi(authedRoutes())
    renderApp("/")
    for (const label of ["Dashboard", "Runners", "History", "Repositories", "Toolchains", "Storage", "Settings"]) {
      expect((await screen.findAllByRole("link", { name: new RegExp(`^${label}`) })).length).toBeGreaterThan(0)
    }
    expect(screen.getByText("Operate")).toBeInTheDocument()
    expect(screen.getByText("Configure")).toBeInTheDocument()
  })

  it("counts live runners and configured repositories in the nav", async () => {
    mockApi(authedRoutes())
    renderApp("/")
    await waitFor(() => expect(within(screen.getByRole("link", { name: /^Runners/ })).getByText("2")).toBeInTheDocument())
    await waitFor(() => expect(within(screen.getByRole("link", { name: /^Repositories/ })).getByText("2")).toBeInTheDocument())
  })

  it("shows reconnecting while status polls fail, until one succeeds", async () => {
    let fail = true
    mockApi(authedRoutes({ "GET /api/status": () => (fail ? json({ error: "connection refused" }, 502) : fixtures.status) }))
    renderApp("/")
    expect(await screen.findByText("Daemon unreachable: connection refused. Retrying.")).toBeInTheDocument()
    fail = false
    await waitFor(() => expect(screen.queryByText(/Daemon unreachable/)).toBeNull(), { timeout: 3000 })
  })

  it("reads a rate-limited status failure as one sentence in the banner", async () => {
    mockApi(authedRoutes({ "GET /api/status": () => json({ error: "GitHub rate limit", retry_at: "2026-10-06T14:20:00Z" }, 429) }))
    renderApp("/")
    expect(await screen.findByText("Daemon unreachable: GitHub rate limit. Retry after 14:20. Retrying.")).toBeInTheDocument()
  })

  it("shows why the daemon is degraded", async () => {
    mockApi(authedRoutes({ "GET /api/status": { ...fixtures.status, degraded: true, degraded_reason: "GitHub rejected the token" } }))
    renderApp("/")
    expect(await screen.findByText("GitHub rejected the token. No new runners start until this clears.")).toBeInTheDocument()
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

  it("logs out from the rail", async () => {
    const { calls } = mockApi(logoutRoutes(noContent))
    const { user, router } = renderApp("/")
    await user.click(await screen.findByRole("button", { name: "Log out" }))
    await waitFor(() => expect(router.state.location.pathname).toBe("/login"))
    expect(calls.some((c) => c.method === "POST" && c.path === "/auth/logout")).toBe(true)
  })

  it("lands on /login even when the logout request fails", async () => {
    const { calls } = mockApi(logoutRoutes(() => json({ error: "session already ended" }, 500)))
    const { user, router } = renderApp("/")
    await user.click(await screen.findByRole("button", { name: "Log out" }))
    await waitFor(() => expect(router.state.location.pathname).toBe("/login"))
    expect(calls.some((c) => c.method === "POST" && c.path === "/auth/logout")).toBe(true)
  })

  it("logs out from the header's user menu past unsaved edits", async () => {
    mockApi(logoutRoutes(noContent))
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
    expect(screen.queryByText(/Daemon unreachable/)).toBeNull()
  })

  it("shows runner capacity and the update card on every page", async () => {
    mockApi(authedRoutes({ "GET /api/history": fixtures.history }))
    renderApp("/history")
    expect((await screen.findAllByText("1 of 2 busy")).length).toBeGreaterThan(0)
    expect(screen.getByRole("img", { name: "1 of 2 busy, 1 warm" })).toBeInTheDocument()
    expect(screen.getByText("Queued since 13:55. Runners update between jobs.")).toBeInTheDocument()
  })

  it("puts the capacity readout and the user menu in the header bar", async () => {
    mockApi(authedRoutes())
    const { container } = renderApp("/")
    await waitFor(() => {
      const header = container.querySelector<HTMLElement>(".dr-layout-header")
      if (!header) throw new Error("no header bar")
      expect(within(header).getByText("1 of 2 busy")).toBeInTheDocument()
      expect(within(header).getByRole("button", { name: "O" })).toBeInTheDocument()
    })
  })

  it("offers dark, light and system in place of the theme switcher", async () => {
    mockApi(authedRoutes())
    renderApp("/")
    expect(await screen.findByRole("radiogroup", { name: "Colour mode" })).toBeInTheDocument()
  })

  it("keeps log out, the colour mode and the update marker out of what a collapsed rail hides", async () => {
    mockApi(authedRoutes())
    renderApp("/")
    const logout = await screen.findByRole("button", { name: "Log out" })
    expect(logout).toHaveAttribute("aria-label", "Log out")
    expect(logout.closest(".ghr-rail-wide")).toBeNull()
    expect(screen.getByRole("radiogroup", { name: "Colour mode" }).closest(".ghr-rail-wide")).toBeNull()
    expect((await screen.findByRole("img", { name: "Runner update needs attention" })).closest(".ghr-rail-wide")).toBeNull()
  })

  it("does not double the period of a degraded reason", async () => {
    mockApi(authedRoutes({ "GET /api/status": { ...fixtures.status, degraded: true, degraded_reason: "GitHub rejected the token." } }))
    renderApp("/")
    expect(await screen.findByText("GitHub rejected the token. No new runners start until this clears.")).toBeInTheDocument()
  })

  it("names the host for the brand label", async () => {
    mockApi(authedRoutes())
    renderApp("/")
    await waitFor(() => expect(document.documentElement.style.getPropertyValue("--ghr-host")).toBe('"localhost"'))
  })
})
