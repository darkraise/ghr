import { screen, waitFor, within } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import type { GhrUpdate } from "@/api/types"
import { mockApi } from "@/test/api"
import { authedRoutes, fixtures } from "@/test/fixtures"
import { renderApp } from "@/test/render"

function routes(g?: Partial<GhrUpdate>) {
  const ghr_update = g ? { installed: "v0.1.25", latest: "v0.1.26", newer: true, ...g } : fixtures.status.ghr_update
  return authedRoutes({ "GET /api/status": { ...fixtures.status, ghr_update } })
}

async function banner() {
  return within(await screen.findByRole("region", { name: "ghr update" }))
}

// The runner update card is up whenever the status has loaded, as the
// fixture has a runner update queued.
async function loaded() {
  await screen.findByRole("region", { name: "Runner update" })
}

describe("ghr update banner", () => {
  beforeEach(() => localStorage.clear())
  afterEach(() => vi.restoreAllMocks())

  it("announces a newer ghr and links to Updates", async () => {
    mockApi(routes({}))
    const { user, router } = renderApp("/")
    const b = await banner()
    expect(b.getByText("ghr v0.1.26 is available. This host runs v0.1.25.")).toBeInTheDocument()
    await user.click(b.getByRole("link", { name: "Open Updates" }))
    await waitFor(() => expect(router.state.location.pathname).toBe("/settings/updates"))
  })

  it("stays hidden while ghr is up to date", async () => {
    mockApi(routes())
    renderApp("/")
    await loaded()
    expect(screen.queryByRole("region", { name: "ghr update" })).toBeNull()
  })

  it("hides a dismissed release until a newer one comes", async () => {
    mockApi(routes({}))
    const view = renderApp("/")
    await view.user.click((await banner()).getByRole("button", { name: "Dismiss" }))
    expect(screen.queryByRole("region", { name: "ghr update" })).toBeNull()
    expect(localStorage.getItem("ghr.dismissedUpdate")).toBe("v0.1.26")
    view.unmount()

    mockApi(routes({}))
    const again = renderApp("/")
    await loaded()
    expect(screen.queryByRole("region", { name: "ghr update" })).toBeNull()
    again.unmount()

    mockApi(routes({ latest: "v0.1.27" }))
    renderApp("/")
    expect((await banner()).getByText("ghr v0.1.27 is available. This host runs v0.1.25.")).toBeInTheDocument()
  })

  it("dismisses for the session when the browser refuses storage", async () => {
    vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => {
      throw new Error("denied")
    })
    vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
      throw new Error("denied")
    })
    mockApi(routes({}))
    const { user } = renderApp("/")
    await user.click((await banner()).getByRole("button", { name: "Dismiss" }))
    expect(screen.queryByRole("region", { name: "ghr update" })).toBeNull()
  })

  it.each([
    [{ installing: true, step: "download" }, "Installing ghr v0.1.26: downloading"],
    [{ installing: true, step: "waiting" }, "Installing ghr v0.1.26: waiting for running work to finish"],
    [{ installing: true, step: "restart", restart_pending: "v0.1.26" }, "Restarting into v0.1.26"],
    [{ restart_pending: "v0.1.26" }, "Restarting into v0.1.26"],
    [{ restart_pending: "v0.1.26", last_outcome: "failed", last_error: "restart: exit status 1" }, "ghr v0.1.26 is installed, but the restart failed."],
  ])("follows an install in %j", async (g, text) => {
    localStorage.setItem("ghr.dismissedUpdate", "v0.1.26")
    mockApi(routes(g))
    renderApp("/")
    const b = await banner()
    expect(b.getByText(text)).toBeInTheDocument()
    expect(b.getByRole("link", { name: "Open Updates" })).toBeInTheDocument()
    expect(b.queryByRole("button", { name: "Dismiss" })).toBeNull()
  })
})
