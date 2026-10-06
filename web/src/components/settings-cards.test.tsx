import { screen, waitFor } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import type { RunnerUpdate } from "@/api/types"
import { dateTime } from "@/lib/format"
import { json, mockApi, noContent } from "@/test/api"
import { authedRoutes, fixtures } from "@/test/fixtures"
import { renderApp } from "@/test/render"

const DAY = 86_400_000

function withUpdate(runner_update: RunnerUpdate, over: Record<string, unknown> = {}) {
  return authedRoutes({ "GET /api/token": fixtures.token, "GET /api/status": { ...fixtures.status, runner_update }, ...over })
}

describe("Maintenance card", () => {
  it("cancels a queued runner update", async () => {
    const { calls } = mockApi(withUpdate(fixtures.status.runner_update, { "DELETE /api/runner-update": () => noContent() }))
    const { user } = renderApp("/settings")
    expect(await screen.findByText("runs when no job is running or queued")).toBeInTheDocument()
    expect(screen.getByText("2.337.0 → 2.338.0")).toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: "Cancel queued update" }))
    await waitFor(() => expect(calls.some((c) => c.method === "DELETE" && c.path === "/api/runner-update")).toBe(true))
    expect((await screen.findAllByText("queued runner update cancelled")).length).toBeGreaterThan(0)
  })

  it("queues an available update and shows its deadline", async () => {
    const deadline = new Date(Date.now() + 20 * DAY + 3_600_000).toISOString()
    const { calls } = mockApi(
      withUpdate({ installed: "2.337.0", latest: "2.338.0", deadline }, { "POST /api/runner-update": () => new Response(null, { status: 202 }) }),
    )
    const { user } = renderApp("/settings")
    expect(await screen.findByText("update available")).toBeInTheDocument()
    expect(screen.getByText(`update by ${dateTime(deadline).slice(0, 10)} (20 days)`)).toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: "Queue update" }))
    await waitFor(() => expect(calls.some((c) => c.method === "POST" && c.path === "/api/runner-update")).toBe(true))
    expect((await screen.findAllByText("runner update queued")).length).toBeGreaterThan(0)
  })

  it("says when the runner is up to date", async () => {
    mockApi(withUpdate({ installed: "2.338.0", checked_at: new Date(Date.now() - 2 * 3_600_000).toISOString() }))
    renderApp("/settings")
    expect(await screen.findByText("up to date")).toBeInTheDocument()
    expect(screen.getByText("checked 2h ago")).toBeInTheDocument()
    expect(screen.queryByRole("button", { name: "Queue update" })).toBeNull()
  })

  it("says when the version is unknown", async () => {
    mockApi(withUpdate({}))
    renderApp("/settings")
    expect(await screen.findByText("version unknown (no dist/current)")).toBeInTheDocument()
  })

  it("shows the last failed check", async () => {
    mockApi(withUpdate({ installed: "2.337.0", check_error: "GitHub: 502" }))
    renderApp("/settings")
    expect(await screen.findByText("last check failed: GitHub: 502")).toBeInTheDocument()
  })

  it("reloads the config and counts the warnings", async () => {
    mockApi(withUpdate({}, { "POST /api/reload": ["web settings changed; restart ghr to apply", "another"] }))
    const { user } = renderApp("/settings")
    await user.click(await screen.findByRole("button", { name: "Reload config.yaml" }))
    expect((await screen.findAllByText("config reloaded (2 warnings, see Activity)")).length).toBeGreaterThan(0)
  })

  it("shows a rejected reload", async () => {
    mockApi(withUpdate({}, { "POST /api/reload": () => json({ error: 'mode must be "queue" or "all"' }, 400) }))
    const { user } = renderApp("/settings")
    await user.click(await screen.findByRole("button", { name: "Reload config.yaml" }))
    expect((await screen.findAllByText('reload rejected: mode must be "queue" or "all"')).length).toBeGreaterThan(0)
  })
})

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
})
