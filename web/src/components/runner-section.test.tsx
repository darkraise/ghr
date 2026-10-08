import { screen, waitFor, within } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import type { RunnerUpdate } from "@/api/types"
import { json, mockApi, noContent } from "@/test/api"
import { authedRoutes, fixtures } from "@/test/fixtures"
import { renderApp } from "@/test/render"

const DAY = 86_400_000
// The UI runs on the daemon's clock, the status fixture's now.
const DAEMON_NOW = Date.parse(fixtures.status.now)

function withUpdate(runner_update: RunnerUpdate, over: Record<string, unknown> = {}) {
  return authedRoutes({ "GET /api/token": fixtures.token, "GET /api/status": { ...fixtures.status, runner_update }, ...over })
}

// The shell's update card repeats Queue and Cancel update outside the page.
async function section() {
  return within(await screen.findByRole("region", { name: "Runner and config" }))
}

describe("Runner and config section", () => {
  it("cancels a queued runner update", async () => {
    const { calls } = mockApi(withUpdate(fixtures.status.runner_update, { "DELETE /api/runner-update": () => noContent() }))
    const { user } = renderApp("/settings")
    const s = await section()
    expect(await s.findByText("2.337.0 to 2.338.0")).toHaveClass("font-mono")
    expect(s.getByText("Queued")).toHaveClass("text-warning")
    expect(s.getByText("Runs when no job is running or queued")).toBeInTheDocument()
    await user.click(s.getByRole("button", { name: "Cancel update" }))
    await waitFor(() => expect(calls.some((c) => c.method === "DELETE" && c.path === "/api/runner-update")).toBe(true))
    expect((await screen.findAllByText("Runner update cancelled")).length).toBeGreaterThan(0)
  })

  it("queues a required update and shows its deadline", async () => {
    const deadline = new Date(DAEMON_NOW + 20 * DAY + 3_600_000).toISOString()
    const { calls } = mockApi(
      withUpdate({ installed: "2.337.0", latest: "2.338.0", deadline }, { "POST /api/runner-update": () => new Response(null, { status: 202 }) }),
    )
    const { user } = renderApp("/settings")
    const s = await section()
    expect(await s.findByText("Update required")).toHaveClass("text-warning")
    expect(s.getByText("Required by Oct 23, in 20 days")).toBeInTheDocument()
    await user.click(s.getByRole("button", { name: "Queue update" }))
    await waitFor(() => expect(calls.some((c) => c.method === "POST" && c.path === "/api/runner-update")).toBe(true))
    expect((await screen.findAllByText("Runner update queued")).length).toBeGreaterThan(0)
  })

  it("turns the required word bad within 7 days, and says when it is overdue", async () => {
    mockApi(withUpdate({ installed: "2.337.0", latest: "2.338.0", deadline: new Date(DAEMON_NOW - 2 * DAY).toISOString() }))
    renderApp("/settings")
    const s = await section()
    expect(await s.findByText("Update required")).toHaveClass("text-destructive")
    expect(s.getByText("Required by Oct 1, overdue")).toBeInTheDocument()
  })

  it("says when the runner is up to date", async () => {
    mockApi(withUpdate({ installed: "2.338.0", checked_at: new Date(DAEMON_NOW - 2 * 3_600_000).toISOString() }))
    renderApp("/settings")
    const s = await section()
    expect(await s.findByText("Up to date")).toHaveClass("text-success")
    expect(s.getByText("checked 2h ago")).toBeInTheDocument()
    expect(s.queryByRole("button", { name: "Queue update" })).toBeNull()
  })

  it.each([
    [{}, "Version unknown. No dist/current link was found."],
    [{ installed: "2.337.0", latest: "2.338.0", running: true }, "Updating"],
    [{ installed: "2.337.0" }, "Checking"],
  ])("reads %j as %s", async (u, text) => {
    mockApi(withUpdate(u))
    renderApp("/settings")
    expect(await (await section()).findByText(text)).toBeInTheDocument()
  })

  it("shows a failed check and a failed update", async () => {
    mockApi(
      withUpdate({
        installed: "2.337.0",
        latest: "2.338.0",
        deadline: new Date(DAEMON_NOW + 20 * DAY).toISOString(),
        check_error: "GitHub: 502",
        last_outcome: "failed",
        last_error: "disk full",
      }),
    )
    renderApp("/settings")
    const s = await section()
    expect(await s.findByText("Last update failed: disk full")).toHaveClass("text-destructive")
    expect(s.getByText("Last check failed: GitHub: 502")).toHaveClass("text-muted-foreground")
  })

  it.each([
    [[], "Config reloaded"],
    [["web settings changed; restart ghr to apply"], "Config reloaded with 1 warning. See Events on the Dashboard."],
    [["a", "b", "c"], "Config reloaded with 3 warnings. See Events on the Dashboard."],
  ])("reloads the config with %j and says so", async (warnings, text) => {
    mockApi(withUpdate({}, { "POST /api/reload": warnings }))
    const { user } = renderApp("/settings")
    await user.click(await (await section()).findByRole("button", { name: "Reload config.yaml" }))
    expect((await screen.findAllByText(text)).length).toBeGreaterThan(0)
  })

  it("shows a rejected reload", async () => {
    mockApi(withUpdate({}, { "POST /api/reload": () => json({ error: 'mode must be "queue" or "all"' }, 400) }))
    const { user } = renderApp("/settings")
    await user.click(await (await section()).findByRole("button", { name: "Reload config.yaml" }))
    expect((await screen.findAllByText('Reload rejected: mode must be "queue" or "all"')).length).toBeGreaterThan(0)
  })

  it("explains what reloading does", async () => {
    mockApi(withUpdate({}))
    renderApp("/settings")
    expect(await (await section()).findByText("Reads config.yaml again. Warnings appear under Events on the Dashboard.")).toBeInTheDocument()
  })
})
