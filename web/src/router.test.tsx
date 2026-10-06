import { screen, waitFor } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import { json, mockApi } from "@/test/api"
import { authedRoutes } from "@/test/fixtures"
import { renderApp } from "@/test/render"

const authed = { "GET /auth/state": { setup_required: false, authenticated: true } }
const anonymous = { "GET /auth/state": { setup_required: false, authenticated: false } }

function renderAt(path: string) {
  return renderApp(path).router
}

describe("routes", () => {
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
    ["/storage", "Storage"],
    ["/settings", "Settings"],
    ["/runners/aaaaaa", "aaaaaa"],
  ])("serves %s", async (path, title) => {
    mockApi(authed)
    renderAt(path)
    expect(await screen.findByRole("heading", { name: title })).toBeInTheDocument()
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

  it("offers a retry when the auth check fails while moving between pages", async () => {
    let down = false
    mockApi(
      authedRoutes({
        "GET /auth/state": () => (down ? json({ error: "connection refused" }, 502) : authed["GET /auth/state"]),
        "GET /api/history": [],
      }),
    )
    const { user } = renderApp("/runners")
    expect(await screen.findByRole("heading", { name: "Runners" })).toBeInTheDocument()
    down = true
    await user.click(screen.getByRole("link", { name: "History" }))
    expect(await screen.findByText("cannot load the page: connection refused")).toBeInTheDocument()
    down = false
    await user.click(screen.getByRole("button", { name: "Retry" }))
    expect(await screen.findByRole("heading", { name: "History" })).toBeInTheDocument()
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
})
