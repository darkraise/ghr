import { screen, waitFor, within } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import type { GhrUpdate, JobStatus, Status } from "@/api/types"
import { mockApi, noContent } from "@/test/api"
import { authedRoutes, fixtures } from "@/test/fixtures"
import { renderApp } from "@/test/render"

const JOBS = fixtures.status.jobs ?? []
const accepted = () => new Response(null, { status: 202 })

function routes(over: Partial<Status> = {}, extra: Record<string, unknown> = {}) {
  return authedRoutes({ "GET /api/token": fixtures.token, "GET /api/status": { ...fixtures.status, ...over }, ...extra })
}

// A status where ghr v0.1.26 is newer than the running v0.1.25, changed by g.
function newer(g: Partial<GhrUpdate>, over: Partial<Status> = {}): Partial<Status> {
  return { ghr_update: { installed: "v0.1.25", latest: "v0.1.26", newer: true, checked_at: fixtures.status.now, ...g }, ...over }
}

// The jobs fixture with the two release checks changed.
function checks(ghr: Partial<JobStatus>, runner: Partial<JobStatus>): JobStatus[] {
  return JOBS.map((j) => (j.name === "ghr_update_check" ? { ...j, ...ghr } : j.name === "runner_update_check" ? { ...j, ...runner } : j))
}

async function card() {
  return within(await screen.findByRole("region", { name: "ghr" }))
}

describe("Settings › Updates", () => {
  it("shows ghr up to date and runs both checks", async () => {
    const { calls } = mockApi(
      routes({}, { "POST /api/jobs/ghr_update_check/run": accepted, "POST /api/jobs/runner_update_check/run": accepted }),
    )
    const { user } = renderApp("/settings/updates")
    expect(await screen.findByRole("heading", { level: 1, name: "Updates" })).toBeInTheDocument()
    const c = await card()
    expect(c.getByText("v0.1.26")).toHaveClass("font-mono")
    expect(c.getByText("Up to date")).toBeInTheDocument()
    expect(c.getByText("checked 1h ago")).toBeInTheDocument()
    expect(screen.getByText("Runner version")).toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: "Check now" }))
    await waitFor(() =>
      expect(calls.filter((x) => x.method === "POST").map((x) => x.path)).toEqual([
        "/api/jobs/ghr_update_check/run",
        "/api/jobs/runner_update_check/run",
      ]),
    )
    expect((await screen.findAllByText("Checking for updates")).length).toBeGreaterThan(0)
  })

  it("runs only the checks that are on", async () => {
    const { calls } = mockApi(routes({ jobs: checks({}, { enabled: false }) }, { "POST /api/jobs/ghr_update_check/run": accepted }))
    const { user } = renderApp("/settings/updates")
    await user.click(await screen.findByRole("button", { name: "Check now" }))
    await waitFor(() => expect(calls.some((x) => x.path === "/api/jobs/ghr_update_check/run")).toBe(true))
    expect(calls.some((x) => x.path === "/api/jobs/runner_update_check/run")).toBe(false)
  })

  it.each([
    [{ enabled: false }, { enabled: false }, "Both checks are off. Turn one on under Jobs."],
    [{ running: true }, {}, "A check is running"],
    [{}, { pending: true }, "A check is running"],
  ])("says why Check now cannot run with %j and %j", async (ghr, runner, reason) => {
    mockApi(routes({ jobs: checks(ghr, runner) }))
    renderApp("/settings/updates")
    expect(await screen.findByText(reason)).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Check now" })).toBeDisabled()
  })

  it("installs a newer ghr after confirming", async () => {
    const { calls } = mockApi(routes(newer({}, { restart: { available: true } }), { "POST /api/ghr-update": accepted }))
    const { user } = renderApp("/settings/updates")
    const c = await card()
    expect(c.getByText("v0.1.25 to v0.1.26")).toBeInTheDocument()
    expect(c.getByText("Update available")).toBeInTheDocument()
    await user.click(c.getByRole("button", { name: "Install v0.1.26" }))
    const dialog = await screen.findByRole("alertdialog")
    expect(within(dialog).getByText("ghr restarts to use v0.1.26. Running jobs keep running.")).toBeInTheDocument()
    await user.click(within(dialog).getByRole("button", { name: "Install" }))
    await waitFor(() => expect(calls.some((x) => x.method === "POST" && x.path === "/api/ghr-update")).toBe(true))
    expect((await screen.findAllByText("Installing ghr v0.1.26")).length).toBeGreaterThan(0)
  })

  it("says why Install is unavailable", async () => {
    mockApi(routes(newer({})))
    renderApp("/settings/updates")
    const c = await card()
    expect(c.getByRole("button", { name: "Install v0.1.26" })).toBeDisabled()
    expect(c.getByText("ghr is not running as ghr.service; upgrade with setup.sh")).toBeInTheDocument()
  })

  it.each([
    [{ installing: true, step: "download" }, [], "Downloading", true],
    [{ installing: true, step: "smoke" }, [], "Testing the new binary", true],
    [{ installing: true, step: "waiting" }, ["pruning"], "Waits for: pruning", true],
    [{ installing: true, step: "swap" }, [], "Installing the files", false],
    [{ installing: true, step: "waiting", restart_pending: "v0.1.26" }, ["pruning"], "Installed, waiting to restart", false],
    [{ installing: true, step: "restart", restart_pending: "v0.1.26" }, [], "Restarting into v0.1.26", false],
    [{ restart_pending: "v0.1.26" }, [], "Restarting into v0.1.26", false],
  ])("follows an install in %j", async (g, blocked, text, cancellable) => {
    mockApi(routes(newer(g, { restart: { available: true, blocked_by: blocked } })))
    renderApp("/settings/updates")
    const c = await card()
    expect(await c.findByText(text)).toBeInTheDocument()
    expect(c.queryByRole("button", { name: "Cancel install" }) !== null).toBe(cancellable)
    expect(c.queryByRole("button", { name: /^Install/ })).toBeNull()
  })

  it("cancels an install before its swap", async () => {
    const { calls } = mockApi(routes(newer({ installing: true, step: "download" }), { "DELETE /api/ghr-update": () => noContent() }))
    const { user } = renderApp("/settings/updates")
    await user.click((await card()).getByRole("button", { name: "Cancel install" }))
    await waitFor(() => expect(calls.some((x) => x.method === "DELETE" && x.path === "/api/ghr-update")).toBe(true))
  })

  it("offers a restart after a failed one", async () => {
    mockApi(
      routes(newer({ restart_pending: "v0.1.26", last_outcome: "failed", last_error: "restart: systemctl restart: exit status 1" }, { restart: { available: true } })),
    )
    renderApp("/settings/updates")
    const c = await card()
    expect(await c.findByText("Restart failed")).toBeInTheDocument()
    expect(c.getByText("Last install failed: restart: systemctl restart: exit status 1")).toHaveClass("text-destructive")
    expect(c.getByRole("button", { name: "Restart ghr" })).toBeInTheDocument()
  })

  it.each([
    [{ installed: "dev", development: true, newer: false }, "Development build. The newest release is v0.1.26."],
    [{ newer: false, latest: undefined, checked_at: undefined, check_error: "GET: 502 Bad Gateway" }, "Last check failed: GET: 502 Bad Gateway"],
    [{ last_outcome: "cancelled" }, "Last install cancelled"],
    [{ installed: "v0.1.26", newer: false, last_outcome: "ok" }, "Updated to v0.1.26"],
    [{ newer: false, checked_at: undefined }, "Checking"],
  ])("reads %j", async (g, text) => {
    mockApi(routes(newer(g)))
    renderApp("/settings/updates")
    expect(await (await card()).findByText(text)).toBeInTheDocument()
  })

  it("links to the check schedules", async () => {
    mockApi(routes())
    const { user, router } = renderApp("/settings/updates")
    await user.click(await screen.findByRole("link", { name: "Check schedules are on the Jobs page" }))
    await waitFor(() => expect(router.state.location.pathname).toBe("/jobs"))
  })

  it("says when the daemon reports neither its version nor its checks", async () => {
    mockApi(routes({ ghr_update: undefined, jobs: undefined }))
    renderApp("/settings/updates")
    expect(await screen.findByText("This daemon does not report its version. Update ghr with setup.sh.")).toBeInTheDocument()
    expect(screen.getByText("This daemon cannot check on request. Update ghr with setup.sh.")).toBeInTheDocument()
  })
})
