import { screen, waitFor, within } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import type { HistoryEntry } from "@/api/types"
import { mockApi, noContent } from "@/test/api"
import { authedRoutes, fixtures } from "@/test/fixtures"
import { renderApp } from "@/test/render"

function entry(id: string, conclusion: string, minutesAgo: number): HistoryEntry {
  const finished = Date.now() - minutesAgo * 60_000
  return {
    id,
    repo: "darkmem",
    run_id: Number(id),
    run_number: id,
    workflow: "ci",
    job_name: "build",
    conclusion,
    started_at: new Date(finished - 5 * 60_000).toISOString(),
    finished_at: new Date(finished).toISOString(),
  }
}

const history = [entry("3", "success", 10), entry("2", "failure", 20), entry("1", "cancelled", 30)]

function routes(over: Record<string, unknown> = {}) {
  return authedRoutes({ "GET /api/history": history, ...over })
}

describe("Repositories page", () => {
  it("shows a card per repo with its state, counts and activity", async () => {
    mockApi(routes())
    renderApp("/repositories")
    expect(await screen.findByText("1/2 running · 3 queued")).toBeInTheDocument()
    expect(screen.getByText("darkcloud")).toBeInTheDocument()
    expect(screen.getByText("removing… running jobs finish first")).toBeInTheDocument()
    expect(screen.getByText("GitHub: not found")).toBeInTheDocument()
    expect(screen.getByText(/#41 build · /)).toBeInTheDocument()
    await waitFor(() => expect(screen.getAllByText("3 jobs · 33% success · avg 5m00s")).toHaveLength(3))
    expect(screen.getAllByRole("img", { name: "#3 build: success" })).toHaveLength(3)
  })

  it("waits for the daemon before the first status", async () => {
    mockApi(routes({ "GET /api/status": () => new Response(JSON.stringify({ error: "connection refused" }), { status: 502 }) }))
    renderApp("/repositories")
    expect(await screen.findByText("waiting for the daemon…")).toBeInTheDocument()
  })

  it("keeps the cards but disables their actions while the daemon is unreachable", { timeout: 10_000 }, async () => {
    let reads = 0
    mockApi(routes({ "GET /api/status": () => (++reads === 1 ? fixtures.status : new Response(JSON.stringify({ error: "connection refused" }), { status: 502 })) }))
    renderApp("/repositories")
    expect(await screen.findByText("Reconnecting", {}, { timeout: 4000 })).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Pause darkmem" })).toBeDisabled()
    expect(screen.getByRole("button", { name: "Remove darkmem" })).toBeDisabled()
  })

  it("says when no repo is configured", async () => {
    mockApi(routes({ "GET /api/status": { ...fixtures.status, repos: [] } }))
    renderApp("/repositories")
    expect(await screen.findByText("No repositories yet")).toBeInTheDocument()
  })

  it("pauses and resumes a repo", async () => {
    const { calls } = mockApi(
      routes({ "POST /api/repos/darkmem/pause": () => noContent(), "POST /api/repos/darkcloud/resume": () => noContent() }),
    )
    const { user } = renderApp("/repositories")
    await user.click(await screen.findByRole("button", { name: "Pause darkmem" }))
    expect((await screen.findAllByText("paused darkmem")).length).toBeGreaterThan(0)
    await user.click(screen.getByRole("button", { name: "Resume darkcloud" }))
    expect((await screen.findAllByText("resumed darkcloud")).length).toBeGreaterThan(0)
    expect(calls.filter((c) => c.method === "POST").map((c) => c.path)).toEqual([
      "/api/repos/darkmem/pause",
      "/api/repos/darkcloud/resume",
    ])
  })

  it("locks a repo that is being removed", async () => {
    mockApi(routes())
    renderApp("/repositories")
    expect(await screen.findByRole("button", { name: "Resume old-repo" })).toBeDisabled()
    expect(screen.getByRole("button", { name: "Remove old-repo" })).toBeDisabled()
  })

  it("removes a repo only after confirmation", async () => {
    const { calls } = mockApi(routes({ "DELETE /api/repos/darkmem": () => noContent() }))
    const { user } = renderApp("/repositories")
    await user.click(await screen.findByRole("button", { name: "Remove darkmem" }))
    expect(await screen.findByText("Remove repo darkmem? Its running jobs finish first.")).toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: "Cancel" }))
    await waitFor(() => expect(screen.queryByRole("alertdialog")).toBeNull())
    expect(calls.some((c) => c.method === "DELETE")).toBe(false)

    await user.click(screen.getByRole("button", { name: "Remove darkmem" }))
    await user.click(within(await screen.findByRole("alertdialog")).getByRole("button", { name: "Remove" }))
    await waitFor(() => expect(calls.some((c) => c.method === "DELETE" && c.path === "/api/repos/darkmem")).toBe(true))
    expect((await screen.findAllByText("removing darkmem")).length).toBeGreaterThan(0)
  })
})
