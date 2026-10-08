import { screen, waitFor, within } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import { json, mockApi, noContent } from "@/test/api"
import { authedRoutes, fixtures } from "@/test/fixtures"
import { renderApp } from "@/test/render"

describe("Account card", () => {
  async function fill(user: ReturnType<typeof renderApp>["user"], current: string, next: string, again: string) {
    await user.type(await screen.findByLabelText("Current password"), current)
    await user.type(screen.getByLabelText("New password"), next)
    await user.type(screen.getByLabelText("Confirm new password"), again)
    await user.click(screen.getByRole("button", { name: "Change password" }))
  }

  it("changes the password", async () => {
    const { calls } = mockApi(authedRoutes({ "GET /api/token": fixtures.token, "POST /auth/password": () => noContent() }))
    const { user } = renderApp("/settings")
    await fill(user, "old password 1", "new password 12", "new password 12")
    await waitFor(() => expect(calls.some((c) => c.path === "/auth/password")).toBe(true))
    expect(calls.find((c) => c.path === "/auth/password")?.body).toEqual({ current: "old password 1", new: "new password 12" })
    expect((await screen.findAllByText("password changed")).length).toBeGreaterThan(0)
    expect(screen.getByLabelText("Current password")).toHaveValue("")
  })

  it("checks the new password before sending", async () => {
    const { calls } = mockApi(authedRoutes({ "GET /api/token": fixtures.token }))
    const { user } = renderApp("/settings")
    await fill(user, "old password 1", "short", "short")
    expect(await screen.findByText("password must be 12 to 1024 bytes")).toBeInTheDocument()
    await user.clear(screen.getByLabelText("New password"))
    await user.type(screen.getByLabelText("New password"), "new password 12")
    await user.click(screen.getByRole("button", { name: "Change password" }))
    expect(await screen.findByText("the passwords do not match")).toBeInTheDocument()
    expect(calls.some((c) => c.path === "/auth/password")).toBe(false)
  })

  it("says when the current password is wrong", async () => {
    mockApi(authedRoutes({ "GET /api/token": fixtures.token, "POST /auth/password": () => json({ error: "wrong password" }, 401) }))
    const { user, router } = renderApp("/settings")
    await fill(user, "bad password 1", "new password 12", "new password 12")
    expect(await screen.findByText("the current password is wrong")).toBeInTheDocument()
    expect(router.state.location.pathname).toBe("/settings")
  })

  it("logs out", async () => {
    let authenticated = true
    const { calls } = mockApi(
      authedRoutes({
        "GET /api/token": fixtures.token,
        "GET /auth/state": () => ({ setup_required: false, authenticated }),
        "POST /auth/logout": () => {
          authenticated = false
          return noContent()
        },
      }),
    )
    const { user, router } = renderApp("/settings")
    await user.click(await screen.findByRole("button", { name: "Log out" }))
    await waitFor(() => expect(router.state.location.pathname).toBe("/login"))
    expect(calls.some((c) => c.method === "POST" && c.path === "/auth/logout")).toBe(true)
  })

  it("logs out without asking about unsaved edits", async () => {
    let authenticated = true
    mockApi(
      authedRoutes({
        "GET /api/token": fixtures.token,
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
    await user.click(within(screen.getByRole("main")).getByRole("button", { name: "Log out" }))
    await waitFor(() => expect(router.state.location.pathname).toBe("/login"))
    expect(screen.queryByText("Unsaved changes")).toBeNull()
  })
})
