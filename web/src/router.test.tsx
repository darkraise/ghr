import { screen, waitFor } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import { mockApi } from "@/test/api"
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
    expect(await screen.findByRole("heading", { name: "Log in" })).toBeInTheDocument()
    expect(router.state.location.pathname).toBe("/login")
  })

  it("sends a logged-in visitor from /login to the dashboard", async () => {
    mockApi(authed)
    const router = renderAt("/login")
    expect(await screen.findByRole("heading", { name: "Dashboard" })).toBeInTheDocument()
    expect(router.state.location.pathname).toBe("/")
  })

  it.each([
    ["/runners", "Runners"],
    ["/history", "History"],
    ["/repositories", "Repositories"],
    ["/storage", "Storage"],
    ["/settings", "Settings"],
    ["/runners/aaaaaa", "Runner"],
  ])("serves %s", async (path, title) => {
    mockApi(authed)
    renderAt(path)
    expect(await screen.findByRole("heading", { name: title })).toBeInTheDocument()
  })

  it("shows the later pages as not in the web UI yet", async () => {
    mockApi(authed)
    renderAt("/storage")
    expect(await screen.findByText("Not in the web UI yet")).toBeInTheDocument()
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
