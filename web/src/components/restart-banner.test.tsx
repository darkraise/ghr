import { renderHook, screen, waitFor, within } from "@testing-library/react"
import { describe, expect, it, onTestFinished, vi } from "vitest"
import { keys } from "@/api/hooks"
import type { Config, RestartStatus, Status } from "@/api/types"
import { useRestarting } from "@/lib/restarting"
import { json, mockApi } from "@/test/api"
import { authedRoutes, fixtures } from "@/test/fixtures"
import { renderApp } from "@/test/render"

const BODY = "Running jobs keep running. The web UI is down for a few seconds and you will need to log in again."

function routes(restart: RestartStatus, over: Partial<Status> = {}, more: Record<string, unknown> = {}) {
  return authedRoutes({ "GET /api/status": { ...fixtures.status, restart, ...over }, "GET /api/token": fixtures.token, ...more })
}

const withWeb = (web: Config["web"]): Config => ({ ...fixtures.config, web })

describe("restart banner", () => {
  it.each([
    [["web.listen"], "Restart needed: web.listen changed."],
    [["web.listen", "web.hosts"], "Restart needed: web.listen and web.hosts changed."],
  ])("names each pending setting (%j)", async (pending, title) => {
    mockApi(routes({ pending, available: true }))
    renderApp("/")
    const banner = await screen.findByRole("region", { name: title })
    expect(banner).toHaveClass("border-warning")
    expect(within(banner).getByRole("button", { name: "Restart ghr" })).toBeEnabled()
  })

  it("is absent while nothing is pending", async () => {
    mockApi(routes({ available: true }))
    const { queryClient } = renderApp("/")
    await waitFor(() => expect(queryClient.getQueryData(keys.status)).toBeDefined())
    expect(screen.queryByRole("region", { name: /^Restart needed/ })).toBeNull()
  })

  it("disables the button and says what the restart waits for", async () => {
    mockApi(routes({ pending: ["web.hosts"], blocked_by: ["installing node 24", "1 queued"], available: true }))
    renderApp("/")
    const banner = await screen.findByRole("region", { name: "Restart needed: web.hosts changed." })
    expect(within(banner).getByText("Waits for: installing node 24, 1 queued")).toBeInTheDocument()
    expect(within(banner).getByRole("button", { name: "Restart ghr" })).toBeDisabled()
  })

  it("says how to restart by hand when ghr is not ghr.service", async () => {
    mockApi(routes({ pending: ["web.hosts"], available: false }))
    renderApp("/")
    const banner = await screen.findByRole("region", { name: "Restart needed: web.hosts changed." })
    expect(within(banner).getByText("Restart ghr with systemctl restart ghr.")).toBeInTheDocument()
    expect(within(banner).queryByRole("button")).toBeNull()
  })

  it("says what the restart waits for even when it must be done by hand", async () => {
    mockApi(routes({ pending: ["web.hosts"], blocked_by: ["pruning"], available: false }))
    renderApp("/")
    const banner = await screen.findByRole("region", { name: "Restart needed: web.hosts changed." })
    expect(within(banner).getByText("Restart ghr with systemctl restart ghr.")).toBeInTheDocument()
    expect(within(banner).getByText("Waits for: pruning")).toBeInTheDocument()
    expect(within(banner).queryByRole("button")).toBeNull()
  })

  it("keeps Restart ghr disabled until the config has loaded", async () => {
    let release: () => void = () => {}
    const loaded = new Promise<void>((resolve) => {
      release = resolve
    })
    mockApi(
      routes({ pending: ["web.listen"], available: true, web_listen: "0.0.0.0:80" }, {}, {
        "GET /api/config": async () => {
          await loaded
          return withWeb({ listen: "0.0.0.0:9090" })
        },
      }),
    )
    renderApp("/")
    const banner = await screen.findByRole("region", { name: "Restart needed: web.listen changed." })
    expect(within(banner).getByRole("button", { name: "Restart ghr" })).toBeDisabled()
    release()
    await waitFor(() => expect(within(banner).getByRole("button", { name: "Restart ghr" })).toBeEnabled())
  })

  it("keeps Restart ghr disabled while the config cannot be read", async () => {
    mockApi(routes({ pending: ["web.listen"], available: true, web_listen: "0.0.0.0:80" }, {}, {
      "GET /api/config": () => json({ error: "config unreadable" }, 500),
    }))
    const { queryClient } = renderApp("/")
    const banner = await screen.findByRole("region", { name: "Restart needed: web.listen changed." })
    await waitFor(() => expect(queryClient.getQueryState(keys.config)?.status).toBe("error"))
    expect(within(banner).getByRole("button", { name: "Restart ghr" })).toBeDisabled()
  })

  it("shows the listener error in its place, with a link to Web access", async () => {
    const scroll = vi.spyOn(Element.prototype, "scrollIntoView")
    onTestFinished(() => scroll.mockRestore())
    const error = { address: "10.0.0.5:8080", reason: "cannot assign requested address", serving_on: "0.0.0.0:8080" }
    mockApi(routes({ pending: ["web.listen"], available: true }, { web_listen_error: error }))
    const { user, router } = renderApp("/")
    const banner = await screen.findByRole("region", {
      name: "ghr could not listen on 10.0.0.5:8080: cannot assign requested address. Serving on 0.0.0.0:8080.",
    })
    expect(banner).toHaveClass("border-destructive")
    expect(screen.queryByRole("region", { name: /^Restart needed/ })).toBeNull()
    await user.click(within(banner).getByRole("link", { name: "Go to Web access" }))
    await waitFor(() => expect(router.state.location.pathname).toBe("/settings"))
    expect(router.state.location.hash).toBe("web-access")
    await waitFor(() => expect(scroll.mock.contexts).toContain(document.getElementById("web-access")))
  })
})

describe("restart confirmation", () => {
  async function ask(restart: RestartStatus, config: Config, more: Record<string, unknown> = {}) {
    const { calls } = mockApi(routes(restart, {}, { "GET /api/config": config, "POST /api/restart": () => new Response(null, { status: 202 }), ...more }))
    const view = renderApp("/")
    await waitFor(() => expect(view.queryClient.getQueryData(keys.config)).toBeDefined())
    await view.user.click(await screen.findByRole("button", { name: "Restart ghr" }))
    const dialog = await screen.findByRole("alertdialog", { name: "Restart ghr?" })
    return { ...view, calls, dialog }
  }

  it.each([
    ["a wildcard", "0.0.0.0:80", withWeb({ listen: "0.0.0.0:9090" }), `${BODY} The web UI moves to http://localhost:9090.`],
    ["a specific IP", "0.0.0.0:80", withWeb({ listen: "192.168.0.99:9090" }), `${BODY} The web UI moves to http://192.168.0.99:9090.`],
    ["a proxy in front", "127.0.0.1:8080", withWeb({ listen: "127.0.0.1:9090" }), `${BODY} If a proxy forwards to ghr, point it at port 9090.`],
    ["an empty listen", "0.0.0.0:80", withWeb({ hosts: ["ghr.lan"] }), `${BODY} The web UI turns off.`],
  ])("says where the web UI goes for %s", async (_name, running, config, text) => {
    const { dialog } = await ask({ pending: ["web.listen"], available: true, web_listen: running }, config)
    expect(within(dialog).getByText(text)).toBeInTheDocument()
    expect(within(dialog).getByRole("button", { name: "Restart" })).toBeInTheDocument()
  })

  it("adds nothing while web.listen is not pending", async () => {
    const { dialog } = await ask({ pending: ["web.hosts"], available: true, web_listen: "0.0.0.0:80" }, withWeb({ listen: "0.0.0.0:9090" }))
    expect(within(dialog).getByText(BODY)).toBeInTheDocument()
  })

  it("restarts once confirmed and records the new address", async () => {
    const { user, calls, dialog } = await ask({ pending: ["web.listen"], available: true, web_listen: "0.0.0.0:80" }, withWeb({ listen: "192.168.0.99:9090" }))
    expect(calls.some((c) => c.path === "/api/restart")).toBe(false)
    await user.click(within(dialog).getByRole("button", { name: "Restart" }))
    await waitFor(() => expect(calls.filter((c) => c.method === "POST" && c.path === "/api/restart")).toHaveLength(1))
    const { result } = renderHook(() => useRestarting())
    await waitFor(() => expect(result.current).toEqual({ url: "http://192.168.0.99:9090", same: false }))
  })

  it("keeps the page's origin when web.listen is not pending", async () => {
    const { user, dialog } = await ask({ pending: ["web.hosts"], available: true, web_listen: "0.0.0.0:80" }, fixtures.config)
    await user.click(within(dialog).getByRole("button", { name: "Restart" }))
    const { result } = renderHook(() => useRestarting())
    await waitFor(() => expect(result.current).toEqual({ url: "http://localhost", same: true }))
  })

  it("shows a refused restart and stays on the page", async () => {
    const { user, dialog } = await ask({ pending: ["web.hosts"], available: true }, fixtures.config, {
      "POST /api/restart": () => json({ error: "waits for: pruning" }, 409),
    })
    await user.click(within(dialog).getByRole("button", { name: "Restart" }))
    expect((await screen.findAllByText("waits for: pruning")).length).toBeGreaterThan(0)
    expect(renderHook(() => useRestarting()).result.current).toBeNull()
    expect(screen.getByRole("region", { name: "Restart needed: web.hosts changed." })).toBeInTheDocument()
  })
})

describe("Web access restart control", () => {
  it("shows Restart ghr under the fields while a setting is pending", async () => {
    mockApi(routes({ pending: ["web.listen"], available: true }))
    renderApp("/settings")
    const group = await screen.findByRole("region", { name: "Web access" })
    expect(await within(group).findByRole("button", { name: "Restart ghr" })).toBeEnabled()
  })

  it("has no restart control while nothing is pending", async () => {
    mockApi(routes({ available: true }))
    const { queryClient } = renderApp("/settings")
    const group = await screen.findByRole("region", { name: "Web access" })
    await waitFor(() => expect(queryClient.getQueryData(keys.status)).toBeDefined())
    expect(within(group).queryByRole("button", { name: "Restart ghr" })).toBeNull()
  })
})
