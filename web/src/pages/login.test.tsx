import { screen, waitFor } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import { json, mockApi, noContent } from "@/test/api"
import { authedRoutes } from "@/test/fixtures"
import { renderApp } from "@/test/render"

const loginState = { "GET /auth/state": { setup_required: false, authenticated: false } }

describe("login page", () => {
  it("sets the first password, checking it first, and opens the dashboard", async () => {
    let done = false
    const { calls } = mockApi(
      authedRoutes({
        "GET /auth/state": () => ({ setup_required: !done, authenticated: done }),
        "POST /auth/setup": () => {
          done = true
          return noContent()
        },
      }),
    )
    const { user, router } = renderApp("/login")
    const password = await screen.findByLabelText("Password")
    const confirm = screen.getByLabelText("Confirm password")
    const submit = screen.getByRole("button", { name: "Set password" })

    await user.type(password, "short")
    await user.type(confirm, "short")
    await user.click(submit)
    expect(screen.getByText("password must be 12 to 1024 bytes")).toBeInTheDocument()

    await user.clear(password)
    await user.clear(confirm)
    await user.type(password, "correct horse battery")
    await user.type(confirm, "correct horse batteries")
    await user.click(submit)
    expect(screen.getByText("the passwords do not match")).toBeInTheDocument()
    expect(calls.some((c) => c.path === "/auth/setup")).toBe(false)

    await user.clear(confirm)
    await user.type(confirm, "correct horse battery")
    await user.click(submit)
    await waitFor(() => expect(router.state.location.pathname).toBe("/"))
    expect(calls.find((c) => c.path === "/auth/setup")?.body).toEqual({ password: "correct horse battery" })
  })

  it("logs in", async () => {
    let done = false
    mockApi(
      authedRoutes({
        "GET /auth/state": () => ({ setup_required: false, authenticated: done }),
        "POST /auth/login": () => {
          done = true
          return noContent()
        },
      }),
    )
    const { user, router } = renderApp("/login")
    await user.type(await screen.findByLabelText("Password"), "correct horse battery")
    await user.click(screen.getByRole("button", { name: "Log in" }))
    await waitFor(() => expect(router.state.location.pathname).toBe("/"))
  })

  it("shows a wrong password inline", async () => {
    mockApi({ ...loginState, "POST /auth/login": () => json({ error: "wrong password" }, 401) })
    const { user, router } = renderApp("/login")
    await user.type(await screen.findByLabelText("Password"), "not the password")
    await user.click(screen.getByRole("button", { name: "Log in" }))
    expect(await screen.findByText("wrong password")).toBeInTheDocument()
    expect(router.state.location.pathname).toBe("/login")
  })

  it("says when to retry after too many failures", async () => {
    mockApi({
      ...loginState,
      "POST /auth/login": () =>
        json({ error: "too many failed logins; retry after 2026-10-06T14:20:00Z", retry_at: "2026-10-06T14:20:00Z" }, 429),
    })
    const { user } = renderApp("/login")
    await user.type(await screen.findByLabelText("Password"), "not the password")
    await user.click(screen.getByRole("button", { name: "Log in" }))
    expect(await screen.findByText("Too many failed logins; try again after 14:20.")).toBeInTheDocument()
  })
})
