import { screen, waitFor, within } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import { mockApi } from "@/test/api"
import { authedRoutes, fixtures } from "@/test/fixtures"
import { setViewport } from "@/test/media"
import { renderApp } from "@/test/render"

const region = (name: string) => within(screen.getByRole("region", { name }))

describe("Actions page", () => {
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
    await screen.findByRole("region", { name: "In progress" })
    const progress = region("In progress")
    const link = progress.getByRole("link", { name: "Fix the cache key, darkmem #42, opens in a new tab" })
    expect(link).toHaveAttribute("href", "https://github.com/darkraise/darkmem/actions/runs/102")
    expect(link).toHaveAttribute("target", "_blank")
    expect(progress.getByText("Running")).toBeInTheDocument()
    expect(progress.getByRole("img", { name: "Queued" })).toBeInTheDocument()
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
    await screen.findByRole("region", { name: "In progress" })
    expect(await region("In progress").findByText(/^5m0\ds$/)).toBeInTheDocument()
    const done = within(region("Recent").getByRole("link", { name: /^Bump the runner/ }).closest("tr") as HTMLElement)
    expect(done.getByText("5m00s")).toBeInTheDocument()
  })

  it("groups the recent runs by day with their result, duration and the ghr mark", async () => {
    mockApi(authedRoutes())
    renderApp("/actions")
    await screen.findByRole("region", { name: "Recent" })
    const recent = region("Recent")
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

  it("lists each repository that could not be read", async () => {
    mockApi(authedRoutes())
    renderApp("/actions")
    expect(await screen.findByText("old-docs: GitHub rate limit; API calls are paused. Retry after 14:30")).toBeInTheDocument()
  })

  it("filters by status in the URL", async () => {
    mockApi(authedRoutes())
    const { user, router } = renderApp("/actions")
    await screen.findByRole("region", { name: "Recent" })
    await user.click(screen.getByRole("radio", { name: "Failed" }))
    await waitFor(() => expect(router.state.location.search).toEqual({ status: "failed" }))
    expect(screen.queryByRole("region", { name: "In progress" })).toBeNull()
    expect(region("Recent").getByRole("link", { name: /^deploy/ })).toBeInTheDocument()
    expect(region("Recent").queryByRole("link", { name: /^Bump the runner/ })).toBeNull()
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
    expect(region("Recent").getByRole("link", { name: /^deploy/ })).toBeInTheDocument()
    expect(region("Recent").queryByRole("link", { name: /^Bump the runner/ })).toBeNull()
  })

  it("picks a watched repository from the select", async () => {
    mockApi(authedRoutes())
    const { user, router } = renderApp("/actions")
    await screen.findByRole("region", { name: "Recent" })
    await user.click(screen.getByRole("combobox", { name: "Repository" }))
    const options = (await screen.findAllByRole("option")).map((o) => o.textContent)
    expect(options).toEqual(["All repositories", "darkmem", "darkcloud", "docs (watched)", "old-docs (watched)"])
    await user.click(screen.getByRole("option", { name: "docs (watched)" }))
    await waitFor(() => expect(router.state.location.search).toEqual({ repo: "docs" }))
    expect(screen.getByText("0 running, 1 queued in docs. Updated 14:05.")).toBeInTheDocument()
    expect(region("In progress").getByRole("link", { name: /^Publish the guide/ })).toBeInTheDocument()
    expect(region("In progress").queryByRole("link", { name: /^Fix the cache key/ })).toBeNull()
    expect(screen.queryByRole("region", { name: "Recent" })).toBeNull()
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
    expect(await screen.findByText("No repositories are covered yet.")).toBeInTheDocument()
    expect(screen.getByRole("link", { name: "Add a repository" })).toHaveAttribute("href", "/repositories")
    expect(screen.getByRole("link", { name: "Watch a repository" })).toHaveAttribute("href", "/repositories#watched")
  })

  it("says when the covered repositories have no runs", async () => {
    mockApi(authedRoutes({ "GET /api/actions": { ...fixtures.actions, runs: [] } }))
    renderApp("/actions")
    expect(await screen.findByText("No workflow runs in the covered repositories yet.")).toBeInTheDocument()
  })

  it("gives duration and start their own columns from 768px", async () => {
    setViewport(1280)
    mockApi(authedRoutes())
    renderApp("/actions")
    await screen.findByRole("region", { name: "Recent" })
    expect(region("Recent").getByRole("columnheader", { name: "Duration" })).toBeInTheDocument()
  })

  it("keeps duration under the facts below 768px", async () => {
    mockApi(authedRoutes())
    renderApp("/actions")
    await screen.findByRole("region", { name: "Recent" })
    expect(region("Recent").queryByRole("columnheader", { name: "Duration" })).toBeNull()
    expect(region("Recent").getByText("5m00s")).toBeInTheDocument()
  })
})
