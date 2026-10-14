import { screen, waitFor, within } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"
import { keys } from "@/api/hooks"
import { liveConfig } from "@/api/live"
import { json, mockApi, noContent } from "@/test/api"
import { authedRoutes, fixtures, runRoutes } from "@/test/fixtures"
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
  it("lists the pages as tabs, without Toolchains or Storage", async () => {
    mockApi(authedRoutes())
    renderApp("/")
    const nav = await screen.findByRole("navigation", { name: "Pages" })
    await waitFor(() => expect(within(nav).getByRole("link", { name: /^Repositories/ })).toBeInTheDocument())
    expect(within(nav).getAllByRole("link").map((a) => a.textContent?.replace(/(, )?\d+$/, ""))).toEqual([
      "Dashboard",
      "Runners",
      "Actions",
      "History",
      "Jobs",
      "Artifacts",
      "Repositories",
      "Settings",
    ])
    expect(within(nav).getByRole("link", { name: /^Dashboard/ })).toHaveAttribute("aria-current", "page")
    expect(within(nav).getByRole("link", { name: /^Settings/ })).toHaveAttribute("href", "/settings")
  })

  it("keeps a tab current on its nested pages", async () => {
    mockApi(runRoutes())
    renderApp("/actions/darkmem/runs/102")
    const nav = await screen.findByRole("navigation", { name: "Pages" })
    expect(within(nav).getByRole("link", { name: /^Actions/ })).toHaveAttribute("aria-current", "page")
    expect(within(nav).getByRole("link", { name: /^Dashboard/ })).not.toHaveAttribute("aria-current")
  })

  it("counts live runners and configured repositories in the tabs", async () => {
    mockApi(authedRoutes())
    renderApp("/")
    const nav = await screen.findByRole("navigation", { name: "Pages" })
    await waitFor(() => expect(within(within(nav).getByRole("link", { name: /^Runners/ })).getByText("2")).toBeInTheDocument())
    await waitFor(() => expect(within(within(nav).getByRole("link", { name: /^Repositories/ })).getByText("2")).toBeInTheDocument())
    expect(within(nav).getByRole("link", { name: /^Runners,\s*2$/ })).toBeInTheDocument()
    expect(within(nav).getByRole("link", { name: /^Repositories,\s*2$/ })).toBeInTheDocument()
  })

  it("clips the tabs' hidden separators inside the tab row", async () => {
    mockApi(authedRoutes())
    renderApp("/")
    const nav = await screen.findByRole("navigation", { name: "Pages" })
    expect(nav).toHaveClass("relative", "overflow-x-auto")
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

  it("logs out from the top bar", async () => {
    const { calls } = mockApi(logoutRoutes(noContent))
    const { user, router } = renderApp("/")
    await user.click(await screen.findByRole("button", { name: "Log out" }))
    await waitFor(() => expect(router.state.location.pathname).toBe("/login"))
    expect(calls.some((c) => c.method === "POST" && c.path === "/auth/logout")).toBe(true)
  })

  it("lands on /login when the session had already ended", async () => {
    const { calls } = mockApi(logoutRoutes(() => json({ error: "not logged in" }, 401)))
    const { user, router } = renderApp("/")
    await user.click(await screen.findByRole("button", { name: "Log out" }))
    await waitFor(() => expect(router.state.location.pathname).toBe("/login"))
    expect(calls.some((c) => c.method === "POST" && c.path === "/auth/logout")).toBe(true)
  })

  it("stays and says so when logout fails", async () => {
    mockApi(authedRoutes({ "POST /auth/logout": () => json({ error: "session store down" }, 500) }))
    const { user, router } = renderApp("/")
    await user.click(await screen.findByRole("button", { name: "Log out" }))
    expect((await screen.findAllByText("Not logged out: session store down")).length).toBeGreaterThan(0)
    expect(router.state.location.pathname).toBe("/")
  })

  it("logs out past unsaved edits", async () => {
    mockApi(logoutRoutes(noContent))
    const { user, router } = renderApp("/settings")
    const poll = await screen.findByLabelText("Poll interval")
    await user.clear(poll)
    await user.type(poll, "15s")
    await user.click(screen.getByRole("button", { name: "Log out" }))
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

  it("puts the brand, the host, the capacity and the mode control in the top bar", async () => {
    mockApi(authedRoutes())
    renderApp("/")
    const bar = await screen.findByRole("banner")
    await waitFor(() => expect(within(bar).getByText("1 of 2 busy")).toBeInTheDocument())
    expect(within(bar).getByRole("img", { name: "1 of 2 busy, 1 warm" })).toBeInTheDocument()
    expect(within(bar).getByText("localhost")).toBeInTheDocument()
    expect(within(bar).getByRole("button", { name: "Dark mode" })).toBeInTheDocument()
    expect(within(bar).getByRole("button", { name: "Log out" })).toBeInTheDocument()
    expect(within(bar).queryByRole("searchbox")).not.toBeInTheDocument()
  })

  it("does not double the period of a degraded reason", async () => {
    mockApi(authedRoutes({ "GET /api/status": { ...fixtures.status, degraded: true, degraded_reason: "GitHub rejected the token." } }))
    renderApp("/")
    expect(await screen.findByText("GitHub rejected the token. No new runners start until this clears.")).toBeInTheDocument()
  })

  it("shows the runner update as a banner above the page", async () => {
    mockApi(authedRoutes())
    renderApp("/")
    const banner = await screen.findByRole("region", { name: "Runner update" })
    expect(screen.getByRole("main")).toContainElement(banner)
    expect(screen.getByRole("banner")).not.toContainElement(banner)
  })

  it("keeps the main-content target and a skip link to it", async () => {
    mockApi(authedRoutes())
    renderApp("/")
    const main = await screen.findByRole("main")
    expect(main).toHaveAttribute("id", "main-content")
    expect(main).toHaveAttribute("tabindex", "-1")
    expect(screen.getByRole("link", { name: "Skip to content" })).toHaveAttribute("href", "#main-content")
  })

  it("opens the change stream once the app is mounted", async () => {
    liveConfig.enabled = true
    try {
      const { calls } = mockApi(
        authedRoutes({
          "GET /api/changes": () =>
            new Response(
              new ReadableStream<Uint8Array>({
                start(c) {
                  c.enqueue(new TextEncoder().encode("event: hello\ndata: {}\n\n"))
                },
              }),
            ),
        }),
      )
      renderApp("/")
      await waitFor(() => expect(calls.some((c) => c.path === "/api/changes")).toBe(true))
    } finally {
      liveConfig.enabled = false
    }
  })
})
