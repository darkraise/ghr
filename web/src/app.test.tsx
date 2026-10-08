import { readFileSync } from "node:fs"
import { dirname, resolve } from "node:path"
import { fileURLToPath } from "node:url"
import { MutationObserver } from "@tanstack/react-query"
import { act, screen, waitFor } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"
import { ApiError, api } from "./api/client"
import { errorText } from "./query"
import { json, mockApi } from "./test/api"
import { authedRoutes, fixtures } from "./test/fixtures"
import { renderApp } from "./test/render"

describe("App", () => {
  it("renders the routed page inside the providers", async () => {
    mockApi(authedRoutes())
    renderApp("/")
    expect(await screen.findByRole("heading", { name: "Dashboard" })).toBeInTheDocument()
  })

  it("sends an expired session from any API call to /login", async () => {
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
    await act(async () => {
      await api.status().catch(() => undefined)
    })
    await waitFor(() => expect(router.state.location.pathname).toBe("/login"))
  })

  it("toasts a failed mutation", async () => {
    mockApi(authedRoutes())
    const { queryClient } = renderApp("/")
    await screen.findByRole("heading", { name: "Dashboard" })
    await act(async () => {
      await new MutationObserver(queryClient, { mutationFn: () => Promise.reject(new Error("boom")) })
        .mutate()
        .catch(() => undefined)
    })
    expect((await screen.findAllByText("boom")).length).toBeGreaterThan(0)
    // At the top, clear of the sticky save bar at the bottom of the form pages.
    expect(document.querySelector(".dr-toaster")).toHaveAttribute("data-position", "top-center")
  })

  it("redirects once for a burst of 401s and keeps the page asked for", async () => {
    let expired = false
    const gone = () => json({ error: "not logged in" }, 401)
    mockApi(
      authedRoutes({
        "GET /auth/state": () => ({ setup_required: false, authenticated: !expired }),
        "GET /api/status": () => (expired ? gone() : fixtures.status),
        "GET /api/config": () => (expired ? gone() : fixtures.config),
        "GET /api/history": [],
      }),
    )
    const { router } = renderApp("/history")
    await screen.findByRole("heading", { name: "History" })
    const navigate = vi.spyOn(router, "navigate")
    expired = true
    await act(async () => {
      await Promise.all([api.status().catch(() => undefined), api.config().catch(() => undefined)])
    })
    await waitFor(() => expect(router.state.location.pathname).toBe("/login"))
    expect(router.state.location.search).toEqual({ redirect: "/history" })
    expect(navigate).toHaveBeenCalledTimes(1)
  })

  it("leaves a page with unsaved edits when the session expires", async () => {
    let expired = false
    mockApi(
      authedRoutes({
        "GET /auth/state": () => ({ setup_required: false, authenticated: !expired }),
        "GET /api/status": () => (expired ? json({ error: "not logged in" }, 401) : fixtures.status),
        "GET /api/token": fixtures.token,
      }),
    )
    const { router, user } = renderApp("/settings")
    const poll = await screen.findByLabelText("Poll interval")
    await user.clear(poll)
    await user.type(poll, "15s")
    expect(await screen.findByText("1 unsaved change")).toBeInTheDocument()
    expired = true
    await act(async () => {
      await api.status().catch(() => undefined)
    })
    await waitFor(() => expect(router.state.location.pathname).toBe("/login"))
    expect(screen.queryByText("Unsaved changes")).toBeNull()
  })
})

describe("errorText", () => {
  it("adds the retry time of a rate-limited request as its own sentence", () => {
    const err = new ApiError(429, "GitHub rate limit; API calls are paused", new Date("2026-10-06T14:20:00Z"))
    expect(errorText(err)).toBe("GitHub rate limit; API calls are paused. Retry after 14:20")
    expect(errorText(new ApiError(429, "Paused.", new Date("2026-10-06T14:20:00Z")))).toBe("Paused. Retry after 14:20")
  })
  it("passes other errors through", () => {
    expect(errorText(new Error("boom"))).toBe("boom")
    expect(errorText("plain")).toBe("plain")
  })
})

describe("index.html", () => {
  const html = readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), "../index.html"), "utf8")

  it("has no inline script, which the CSP forbids", () => {
    expect(html).not.toMatch(/<script(?![^>]*\bsrc=)[^>]*>/)
  })

  it("loads nothing from another origin", () => {
    expect(html).not.toMatch(/https?:\/\//)
  })

  it("is titled ghr", () => {
    expect(html).toContain("<title>ghr</title>")
  })
})
