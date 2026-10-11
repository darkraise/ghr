import { screen, waitFor, within } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import { json, mockApi } from "@/test/api"
import { authedRoutes, fixtures, runRoutes } from "@/test/fixtures"
import { setViewport } from "@/test/media"
import { renderApp } from "@/test/render"

const box = () => within(screen.getByRole("region", { name: /workflow runs?$/ }))

describe("Actions page", () => {
  it("lists every run in one box whose header counts them and holds the filters", async () => {
    setViewport(1024)
    mockApi(authedRoutes())
    renderApp("/actions")
    const total = fixtures.actions.runs.length
    const box = await screen.findByRole("region", { name: `${total} workflow runs` })
    expect(within(box).getByRole("combobox", { name: "Repository" })).toBeInTheDocument()
    expect(within(box).getByRole("radiogroup", { name: "Status" })).toBeInTheDocument()
    expect(within(box).getAllByRole("table")).toHaveLength(1)
    expect(screen.queryByRole("region", { name: "Recent" })).not.toBeInTheDocument()
    expect(screen.queryByRole("region", { name: "In progress" })).not.toBeInTheDocument()
  })

  it("holds the filters in the box from the first load on, so they do not move", async () => {
    let answer: (value: unknown) => void = () => {}
    mockApi(authedRoutes({ "GET /api/actions": () => new Promise((resolve) => (answer = resolve)) }))
    renderApp("/actions")
    const loading = await screen.findByRole("region", { name: "Workflow runs" })
    const status = within(loading).getByRole("radiogroup", { name: "Status" })
    expect(within(loading).getByRole("combobox", { name: "Repository" })).toBeInTheDocument()
    expect(within(loading).getByText("Loading")).toBeInTheDocument()
    answer(fixtures.actions)
    const listed = await screen.findByRole("region", { name: `${fixtures.actions.runs.length} workflow runs` })
    expect(listed).toBe(loading)
    expect(within(listed).getByRole("radiogroup", { name: "Status" })).toBe(status)
  })

  it("keeps the filters in the box when the first load fails", async () => {
    mockApi(authedRoutes({ "GET /api/actions": () => json({ error: "bad gateway" }, 502) }))
    renderApp("/actions")
    const box = await screen.findByRole("region", { name: "Workflow runs" })
    expect(await within(box).findByText("bad gateway")).toBeInTheDocument()
    expect(within(box).getByRole("radiogroup", { name: "Status" })).toBeInTheDocument()
  })

  it("counts only the runs the filters leave", async () => {
    setViewport(1024)
    mockApi(authedRoutes())
    const repo = fixtures.actions.runs.map((r) => r.repo)[0]
    const left = fixtures.actions.runs.filter((r) => r.repo === repo).length
    renderApp(`/actions?repo=${repo}`)
    expect(await screen.findByRole("region", { name: `${left} workflow ${left === 1 ? "run" : "runs"}` })).toBeInTheDocument()
  })

  it("heads the groups inside the box with row headers", async () => {
    setViewport(1024)
    mockApi(authedRoutes())
    renderApp("/actions")
    const box = await screen.findByRole("region", { name: /workflow runs?$/ })
    const headings = within(box).getAllByRole("rowheader")
    expect(headings.length).toBeGreaterThan(0)
    for (const h of headings) expect(h).toHaveAttribute("scope", "rowgroup")
    const active = fixtures.actions.runs.some((r) => r.status !== "completed")
    expect(headings[0]).toHaveTextContent(active ? "In progress" : /.+/)
  })

  it("writes a run's facts and values in the row", async () => {
    setViewport(1024)
    mockApi(authedRoutes())
    renderApp("/actions")
    const run = fixtures.actions.runs.find((r) => r.branch && r.actor && r.event)
    if (!run) throw new Error("fixture has no run with a branch, actor and event")
    const escaped = run.title.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")
    const [link] = await screen.findAllByRole("link", { name: new RegExp(`^${escaped}`) })
    const row = within(link?.closest("tr") as HTMLElement)
    expect(row.getByText(`${run.workflow} #${run.run_number}`)).toBeInTheDocument()
    expect(row.getByText(run.repo)).toBeInTheDocument()
    expect(row.getByText(`${run.event} by ${run.actor}`)).toBeInTheDocument()
    expect(row.getByText(run.branch)).toHaveClass("font-mono", "text-primary", "rounded-full", "bg-branch")
    expect(row.queryAllByText("ghr")).toHaveLength(run.ghr ? 1 : 0)
    expect(row.getAllByRole("cell")).toHaveLength(3)
  })

  it("keeps the filters, and focus, in the box when they hide every run", async () => {
    setViewport(1024)
    mockApi(authedRoutes())
    const { user } = renderApp("/actions?repo=docs")
    await screen.findByRole("region", { name: "1 workflow run" })
    const failed = screen.getByRole("radio", { name: "Failed" })
    await user.click(failed)
    const box = await screen.findByRole("region", { name: "0 workflow runs" })
    expect(within(box).getByText("No runs match these filters.")).toBeInTheDocument()
    expect(within(box).getByRole("radio", { name: "Failed" })).toBe(failed)
    expect(failed).toHaveFocus()
  })

  it("sums up the runs and is listed in the nav", async () => {
    const { calls } = mockApi(authedRoutes())
    renderApp("/actions")
    expect(await screen.findByRole("heading", { name: "Actions" })).toBeInTheDocument()
    expect(await screen.findByText("1 running, 1 queued across 4 repositories. Updated 14:05.")).toBeInTheDocument()
    expect((await screen.findAllByRole("link", { name: /^Actions/ })).length).toBeGreaterThan(0)
    expect(calls.some((c) => c.path === "/api/actions")).toBe(true)
  })

  it("shows the runs in progress with their elapsed time", async () => {
    mockApi(authedRoutes())
    renderApp("/actions")
    await screen.findByRole("region", { name: /workflow runs?$/ })
    const progress = box()
    const link = progress.getByRole("link", { name: "Fix the cache key, darkmem #42" })
    expect(link).toHaveAttribute("href", "/actions/darkmem/runs/102")
    expect(link).not.toHaveAttribute("target")
    expect(progress.getByText("Running")).toBeInTheDocument()
    expect(progress.getByRole("img", { name: "Waiting" })).toBeInTheDocument()
    expect(progress.getByText(/^4m0\ds$/)).toBeInTheDocument()
  })

  it("ticks the elapsed time of a running run with the daemon's clock", { timeout: 10_000 }, async () => {
    let polls = 0
    mockApi(
      authedRoutes({
        "GET /api/status": () => ({ ...fixtures.status, now: polls++ < 1 ? fixtures.status.now : "2026-10-03T14:06:00Z" }),
      }),
    )
    renderApp("/actions")
    await screen.findByRole("region", { name: /workflow runs?$/ })
    expect(await box().findByText(/^5m0\ds$/)).toBeInTheDocument()
    const done = within(box().getByRole("link", { name: /^Bump the runner/ }).closest("tr") as HTMLElement)
    expect(done.getByText("5m00s")).toBeInTheDocument()
  })

  it("groups the recent runs by day with their result, duration and the ghr mark", async () => {
    mockApi(authedRoutes())
    renderApp("/actions")
    await screen.findByRole("region", { name: /workflow runs?$/ })
    const recent = box()
    expect(recent.getByText("Today")).toBeInTheDocument()
    expect(recent.getByText("Yesterday")).toBeInTheDocument()
    const row = within(recent.getByRole("link", { name: /^Bump the runner/ }).closest("tr") as HTMLElement)
    expect(row.getByRole("img", { name: "Succeeded" })).toBeInTheDocument()
    expect(row.getByText("5m00s")).toBeInTheDocument()
    expect(row.getByText("ghr")).toHaveClass("font-mono")
    expect(row.getByText("renovate/runner")).toHaveClass("font-mono")
    const old = within(recent.getByRole("link", { name: /^deploy/ }).closest("tr") as HTMLElement)
    expect(old.getByText("Oct 2 14:05")).toBeInTheDocument()
    expect(old.queryByText("ghr")).toBeNull()
  })

  it("opens a run's page from its title", async () => {
    mockApi(runRoutes())
    const { user, router } = renderApp("/actions")
    await screen.findByRole("region", { name: /workflow runs?$/ })
    await user.click(box().getByRole("link", { name: "Fix the cache key, darkmem #42" }))
    expect(await screen.findByRole("heading", { name: "Fix the cache key", level: 1 })).toBeInTheDocument()
    expect(router.state.location.pathname).toBe("/actions/darkmem/runs/102")
  })

  it("lists each repository that could not be read", async () => {
    mockApi(authedRoutes())
    renderApp("/actions")
    expect(await screen.findByText("old-docs: GitHub rate limit; API calls are paused. Retry after 14:30")).toBeInTheDocument()
  })

  it("keeps the runs and shows the error when a later poll fails", async () => {
    let down = false
    mockApi(authedRoutes({ "GET /api/actions": () => (down ? json({ error: "bad gateway" }, 502) : fixtures.actions) }))
    const { queryClient } = renderApp("/actions")
    await screen.findByRole("region", { name: /workflow runs?$/ })
    down = true
    await queryClient.invalidateQueries({ queryKey: ["actions"] })
    expect(await screen.findByText("bad gateway")).toBeInTheDocument()
    expect(box().getByRole("link", { name: /^Bump the runner/ })).toBeInTheDocument()
  })

  it("filters by status in the URL", async () => {
    mockApi(authedRoutes())
    const { user, router } = renderApp("/actions")
    await screen.findByRole("region", { name: /workflow runs?$/ })
    await user.click(screen.getByRole("radio", { name: "Failed" }))
    await waitFor(() => expect(router.state.location.search).toEqual({ status: "failed" }))
    expect(screen.queryByRole("rowheader", { name: "In progress" })).toBeNull()
    expect(box().getByRole("link", { name: /^deploy/ })).toBeInTheDocument()
    expect(box().queryByRole("link", { name: /^Bump the runner/ })).toBeNull()
  })

  it.each([
    ["only finished runs", fixtures.actions.runs.slice(2)],
    ["no runs at all", []],
  ])("says nothing is running when Active finds nothing, with %s", async (_label, runs) => {
    mockApi(authedRoutes({ "GET /api/actions": { ...fixtures.actions, runs } }))
    renderApp("/actions?status=active")
    expect(await screen.findByText("Nothing is running or queued.")).toBeInTheDocument()
  })

  it("filters by a configured or watched repository", async () => {
    mockApi(authedRoutes())
    renderApp("/actions?repo=darkcloud")
    expect(await screen.findByText("0 running, 0 queued in darkcloud. Updated 14:05.")).toBeInTheDocument()
    expect(box().getByRole("link", { name: /^deploy/ })).toBeInTheDocument()
    expect(box().queryByRole("link", { name: /^Bump the runner/ })).toBeNull()
  })

  it("picks a watched repository from the select", async () => {
    mockApi(authedRoutes())
    const { user, router } = renderApp("/actions")
    await screen.findByRole("region", { name: /workflow runs?$/ })
    await user.click(screen.getByRole("combobox", { name: "Repository" }))
    const options = (await screen.findAllByRole("option")).map((o) => o.textContent)
    expect(options).toEqual(["All repositories", "darkmem", "darkcloud", "docs (watched)", "old-docs (watched)"])
    await user.click(screen.getByRole("option", { name: "docs (watched)" }))
    await waitFor(() => expect(router.state.location.search).toEqual({ repo: "docs" }))
    expect(screen.getByText("0 running, 1 queued in docs. Updated 14:05.")).toBeInTheDocument()
    expect(box().getByRole("link", { name: /^Publish the guide/ })).toBeInTheDocument()
    expect(box().queryByRole("link", { name: /^Fix the cache key/ })).toBeNull()
    expect(box().getAllByRole("rowheader")).toHaveLength(1)
  })

  it("drops values it does not know, from the page and the URL", async () => {
    mockApi(authedRoutes())
    const { router } = renderApp("/actions?repo=nope&status=bogus")
    expect(await screen.findByText("1 running, 1 queued across 4 repositories. Updated 14:05.")).toBeInTheDocument()
    await waitFor(() => expect(router.state.location.search).toEqual({}))
    expect(router.state.location.href).toBe("/actions")
  })

  it("offers a way back when the filters hide everything", async () => {
    mockApi(authedRoutes())
    const { user, router } = renderApp("/actions?repo=docs&status=failed")
    expect(await screen.findByText("No runs match these filters.")).toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: "Clear filters" }))
    await waitFor(() => expect(router.state.location.search).toEqual({}))
  })

  it("explains an empty page", async () => {
    mockApi(authedRoutes({ "GET /api/actions": { fetched_at: "2026-10-03T14:05:00Z", runs: [], repos: [] } }))
    renderApp("/actions")
    const box = await screen.findByRole("region", { name: "0 workflow runs" })
    expect(within(box).getByText("No repositories are covered yet.")).toBeInTheDocument()
    expect(within(box).getByRole("radiogroup", { name: "Status" })).toBeInTheDocument()
    expect(screen.getByRole("link", { name: "Add a repository" })).toHaveAttribute("href", "/repositories")
    expect(screen.getByRole("link", { name: "Watch a repository" })).toHaveAttribute("href", "/repositories#watched")
  })

  it("says when the covered repositories have no runs", async () => {
    mockApi(authedRoutes({ "GET /api/actions": { ...fixtures.actions, runs: [] } }))
    renderApp("/actions")
    expect(await screen.findByText("No workflow runs in the covered repositories yet.")).toBeInTheDocument()
  })

  it("gives duration and start their own cell from 768px", async () => {
    setViewport(1280)
    mockApi(authedRoutes())
    renderApp("/actions")
    await screen.findByRole("region", { name: /workflow runs?$/ })
    const row = within(box().getByRole("link", { name: /^Bump the runner/ }).closest("tr") as HTMLElement)
    expect(row.getAllByRole("cell")).toHaveLength(3)
    expect(row.getByText("5m00s").closest("td")).toBe(row.getAllByRole("cell")[2])
  })

  it("keeps duration under the facts below 768px", async () => {
    mockApi(authedRoutes())
    renderApp("/actions")
    await screen.findByRole("region", { name: /workflow runs?$/ })
    const row = within(box().getByRole("link", { name: /^Bump the runner/ }).closest("tr") as HTMLElement)
    expect(row.getAllByRole("cell")).toHaveLength(2)
    expect(row.getByText("5m00s")).toBeInTheDocument()
  })
})
