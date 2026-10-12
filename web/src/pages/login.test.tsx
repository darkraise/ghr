import { screen, waitFor } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"
import { json, mockApi, noContent } from "@/test/api"
import { authedRoutes, fixtures } from "@/test/fixtures"
import { renderApp } from "@/test/render"

const loginState = { "GET /auth/state": { setup_required: false, authenticated: false } }

describe("login page", () => {
  it("sets the first password, checking it first, and opens the dashboard", { timeout: 15_000 }, async () => {
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
    const assign = vi.fn()
    vi.stubGlobal("location", { assign })
    const { user } = renderApp("/login")
    const code = await screen.findByLabelText("Setup code")
    const password = screen.getByLabelText("Password")
    const confirm = screen.getByLabelText("Confirm password")
    const submit = screen.getByRole("button", { name: "Set password" })
    expect(
      screen.getByText("No password is set. Enter the setup code from this host and choose one to claim this ghr."),
    ).toBeInTheDocument()
    expect(code).toHaveAttribute("autocomplete", "one-time-code")
    expect(code).toHaveAccessibleDescription("Printed by setup.sh. On the host, run ghr code.")
    await user.type(code, "abcde-fghjk-mnpqr-stvwx")

    await user.type(password, "short")
    await user.type(confirm, "short")
    await user.click(submit)
    expect(screen.getByText("password must be 12 to 1024 characters")).toBeInTheDocument()

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
    await waitFor(() => expect(assign).toHaveBeenCalledWith("/"))
    expect(calls.find((c) => c.path === "/auth/setup")?.body).toEqual({
      code: "abcde-fghjk-mnpqr-stvwx",
      password: "correct horse battery",
    })
  })

  it("shows a refused setup code inline", async () => {
    mockApi({
      "GET /auth/state": { setup_required: true, authenticated: false },
      "POST /auth/setup": () => json({ error: "wrong setup code; run on the host: ghr code" }, 401),
    })
    const { user, router } = renderApp("/login")
    await user.type(await screen.findByLabelText("Setup code"), "not-the-code")
    await user.type(screen.getByLabelText("Password"), "correct horse battery")
    await user.type(screen.getByLabelText("Confirm password"), "correct horse battery")
    await user.click(screen.getByRole("button", { name: "Set password" }))
    expect(await screen.findByRole("alert")).toHaveTextContent("wrong setup code; run on the host: ghr code")
    expect(router.state.location.pathname).toBe("/login")
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
    const assign = vi.fn()
    vi.stubGlobal("location", { assign })
    const { user } = renderApp("/login")
    await user.type(await screen.findByLabelText("Password"), "correct horse battery")
    expect(screen.queryByLabelText("Setup code")).not.toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: "Log in" }))
    await waitFor(() => expect(assign).toHaveBeenCalledWith("/"))
    expect(screen.getByRole("button", { name: "Log in" })).toBeDisabled()
  })

  it("shows a wrong password inline", async () => {
    mockApi({ ...loginState, "POST /auth/login": () => json({ error: "wrong password" }, 401) })
    const { user, router } = renderApp("/login")
    await user.type(await screen.findByLabelText("Password"), "not the password")
    await user.click(screen.getByRole("button", { name: "Log in" }))
    expect(await screen.findByText("wrong password")).toBeInTheDocument()
    expect(screen.getByRole("alert")).toHaveTextContent("wrong password")
    expect(router.state.location.pathname).toBe("/login")
  })

  it.each([
    [true, "Set password", "POST /auth/setup", "a web password is already set", "Log in"],
    [false, "Log in", "POST /auth/login", "no web password is set yet; set one first", "Set password"],
  ])("changes mode after a 409 (setup required: %s)", async (required, submit, route, error, next) => {
    let setup = required
    mockApi({
      "GET /auth/state": () => ({ setup_required: setup, authenticated: false }),
      [route]: () => {
        setup = !required
        return json({ error }, 409)
      },
    })
    const { user } = renderApp("/login")
    await user.type(await screen.findByLabelText("Password"), "correct horse battery")
    if (required) {
      await user.type(screen.getByLabelText("Setup code"), "abcde-fghjk-mnpqr-stvwx")
      await user.type(screen.getByLabelText("Confirm password"), "correct horse battery")
    }
    await user.click(screen.getByRole("button", { name: submit }))
    expect(await screen.findByRole("button", { name: next })).toBeInTheDocument()
    expect(screen.getByRole("alert")).toHaveTextContent(error)
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

  it("returns to the page asked for after logging in", async () => {
    let done = false
    mockApi(
      authedRoutes({
        "GET /auth/state": () => ({ setup_required: false, authenticated: done }),
        "POST /auth/login": () => {
          done = true
          return noContent()
        },
        "GET /api/history": fixtures.history,
      }),
    )
    const { user, router } = renderApp("/history")
    await user.type(await screen.findByLabelText("Password"), "correct horse battery")
    expect(router.state.location.pathname).toBe("/login")
    expect(router.state.location.search).toEqual({ redirect: "/history" })
    const assign = vi.fn()
    vi.stubGlobal("location", { assign })
    await user.click(screen.getByRole("button", { name: "Log in" }))
    await waitFor(() => expect(assign).toHaveBeenCalledWith("/history"))
  })

  // The browser drops tabs and newlines from a URL, so "/<tab>/host" is "//host",
  // and removing dot segments turns "/.//host" into "//host".
  const offSite = [
    "//evil.example/x",
    "https://evil.example/x",
    "/\\evil.example/x",
    "/\t/evil.example/x",
    "/\n/evil.example/x",
    "/\r\\evil.example/x",
    "/.//evil.example/x",
    "/a/..//evil.example/x",
    "/%2e//evil.example/x",
  ]

  it.each(offSite)("ignores the redirect %j that leaves the site", async (target) => {
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
    const assign = vi.fn()
    vi.stubGlobal("location", { assign })
    const { user } = renderApp(`/login?redirect=${encodeURIComponent(target)}`)
    await user.type(await screen.findByLabelText("Password"), "correct horse battery")
    await user.click(screen.getByRole("button", { name: "Log in" }))
    await waitFor(() => expect(assign).toHaveBeenCalledWith("/"))
  })
})
