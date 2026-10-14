import { screen, waitFor, within } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import type { Config, ConfigPatch, JobName, JobsConfig, JobStatus } from "@/api/types"
import { json, mockApi, noContent } from "@/test/api"
import { authedRoutes, fixtures } from "@/test/fixtures"
import { renderApp } from "@/test/render"
import { OFF_WARNINGS } from "./jobs"

const JOBS = fixtures.status.jobs ?? []

function job(name: string, over: Partial<JobStatus> = {}): JobStatus {
  const j = JOBS.find((x) => x.name === name)
  if (!j) throw new Error(`no fixture job ${name}`)
  return { ...j, ...over }
}

function routes(jobs: JobStatus[] | undefined, over: Record<string, unknown> = {}) {
  return authedRoutes({ "GET /api/status": { ...fixtures.status, jobs }, ...over })
}

// A daemon that records each PATCH /config and serves the jobs block it sets.
function savingDaemon(patches: ConfigPatch[]) {
  let config: Config = fixtures.config
  return routes(JOBS, {
    "GET /api/config": () => config,
    "PATCH /api/config": ({ body }: { body: unknown }) => {
      const patch = body as ConfigPatch
      patches.push(patch)
      const jobs = { ...(config.jobs as JobsConfig) }
      for (const [name, p] of Object.entries(patch.jobs ?? {})) jobs[name as JobName] = { ...jobs[name as JobName], ...p }
      config = { ...config, jobs }
      return noContent()
    },
  })
}

async function task(title: string) {
  return within(await screen.findByRole("region", { name: title }))
}

describe("Jobs page", () => {
  it("lists the background tasks with their last and next run", async () => {
    mockApi(routes(JOBS))
    renderApp("/jobs")
    expect(await screen.findByRole("heading", { level: 1, name: "Jobs" })).toBeInTheDocument()
    const prune = await task("Prune")
    expect(prune.getByText("Removes history and logs older than history_retention. While on, a disk above disk_high_water also prunes Docker.")).toBeInTheDocument()
    expect(prune.getByText("Ran 21h ago")).toBeInTheDocument()
    expect(prune.getByText("In 2h00m")).toBeInTheDocument()
    expect(prune.getByRole("switch", { name: "Run Prune automatically" })).toBeChecked()
    expect(await prune.findByLabelText("Prune every")).toHaveValue("24")
    expect(prune.getByLabelText("Prune every")).toHaveAttribute("aria-valuetext", "24 hours")
    const storage = await task("Storage measurement")
    expect(storage.getByLabelText("Storage measurement every")).toHaveValue("30")
    expect(storage.getByText("Ran 14m ago")).toBeInTheDocument()
    expect((await task("ghr update check")).getByText("In 23h00m")).toBeInTheDocument()
    expect(await task("Runner update check")).toBeTruthy()
  })

  it("runs a task now, and says why one cannot run", async () => {
    const { calls } = mockApi(
      routes(
        [
          job("ghr_update_check"),
          job("runner_update_check", { running: true, next: undefined }),
          job("prune", { pending: true }),
          job("storage_measure", { enabled: false, next: undefined }),
        ],
        { "POST /api/jobs/ghr_update_check/run": () => new Response(null, { status: 202 }) },
      ),
    )
    const { user } = renderApp("/jobs")
    const runner = await task("Runner update check")
    expect(runner.getByText("Running")).toBeInTheDocument()
    expect(runner.getByRole("button", { name: "Run now" })).toBeDisabled()
    expect(runner.getByText("Running now")).toBeInTheDocument()
    const prune = await task("Prune")
    expect(prune.getByText("Starting")).toBeInTheDocument()
    expect(prune.getByRole("button", { name: "Run now" })).toBeDisabled()
    expect(prune.getByText("Starts at the next pass")).toBeInTheDocument()
    const storage = await task("Storage measurement")
    expect(storage.getByText("Off")).toBeInTheDocument()
    expect(storage.getByRole("button", { name: "Run now" })).toBeDisabled()
    expect(storage.getByText("Turn it on and save to run it")).toBeInTheDocument()
    await user.click((await task("ghr update check")).getByRole("button", { name: "Run now" }))
    await waitFor(() => expect(calls.some((c) => c.method === "POST" && c.path === "/api/jobs/ghr_update_check/run")).toBe(true))
    expect((await screen.findAllByText("ghr update check started")).length).toBeGreaterThan(0)
  })

  it("shows a refused Run now", async () => {
    mockApi(routes(JOBS, { "POST /api/jobs/prune/run": () => json({ error: "prune is already running or about to run" }, 409) }))
    const { user } = renderApp("/jobs")
    await user.click((await task("Prune")).getByRole("button", { name: "Run now" }))
    expect((await screen.findAllByText("Not started: prune is already running or about to run")).length).toBeGreaterThan(0)
  })

  it("saves an interval and a switch, counting only its own changes", async () => {
    const patches: ConfigPatch[] = []
    mockApi(savingDaemon(patches))
    const { user } = renderApp("/jobs")
    const every = (await task("Prune")).getByLabelText("Prune every")
    await waitFor(() => expect(every).toHaveValue("24"))
    await user.clear(every)
    await user.type(every, "12")
    await user.click((await task("Storage measurement")).getByRole("switch", { name: "Run Storage measurement automatically" }))
    expect(screen.getByText("2 unsaved changes")).toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: "Save changes" }))
    await waitFor(() => expect(patches).toEqual([{ jobs: { prune: { every: "12h" }, storage_measure: { enabled: false } } }]))
    await waitFor(() => expect(screen.queryByText(/unsaved change/)).toBeNull())
  })

  it("refuses an interval under the floor before saving", async () => {
    mockApi(routes(JOBS))
    const { user } = renderApp("/jobs")
    const every = (await task("Storage measurement")).getByLabelText("Storage measurement every")
    await waitFor(() => expect(every).toHaveValue("30"))
    await user.clear(every)
    await user.type(every, "4")
    expect(await screen.findByText("Enter at least 5 minutes")).toBeInTheDocument()
  })

  it("places a refused interval under its field", async () => {
    mockApi(routes(JOBS, { "PATCH /api/config": () => json({ error: 'jobs.prune.every: invalid duration "12h"' }, 400) }))
    const { user } = renderApp("/jobs")
    const every = (await task("Prune")).getByLabelText("Prune every")
    await waitFor(() => expect(every).toHaveValue("24"))
    await user.clear(every)
    await user.type(every, "12")
    await user.click(screen.getByRole("button", { name: "Save changes" }))
    expect(await (await task("Prune")).findByText('invalid duration "12h"')).toBeInTheDocument()
  })

  // OFF_WARNINGS holds the words of config.RunnerCheckOffReason and
  // config.PruneOffReason, which config.Validate warns with.
  it("warns with the daemon's words when a task is turned off", async () => {
    expect(OFF_WARNINGS).toEqual({
      runner_update_check:
        "GitHub stops assigning jobs to a runner past its forced-update deadline, and ghr no longer learns of new runner releases or deadlines",
      prune: "history and logs older than history_retention are kept, and a full disk is no longer pruned automatically",
    })
    mockApi(routes(JOBS))
    const { user } = renderApp("/jobs")
    const prune = await task("Prune")
    await user.click(prune.getByRole("switch", { name: "Run Prune automatically" }))
    expect(prune.getByText("History and logs older than history_retention are kept, and a full disk is no longer pruned automatically.")).toBeInTheDocument()
    const runner = await task("Runner update check")
    await user.click(runner.getByRole("switch", { name: "Run Runner update check automatically" }))
    expect(
      runner.getByText(
        "GitHub stops assigning jobs to a runner past its forced-update deadline, and ghr no longer learns of new runner releases or deadlines.",
      ),
    ).toBeInTheDocument()
  })

  it("says when the daemon reports no background tasks", async () => {
    mockApi(routes(undefined))
    renderApp("/jobs")
    expect(await screen.findByText("This daemon reports no background tasks. Update ghr to manage them here.")).toBeInTheDocument()
  })
})
