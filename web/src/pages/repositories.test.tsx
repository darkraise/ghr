import { screen, waitFor, within } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import { json, mockApi, noContent } from "@/test/api"
import { authedRoutes, fixtures } from "@/test/fixtures"
import { renderApp } from "@/test/render"

const row = (name: string) => within(screen.getByRole("link", { name }).closest("tr") as HTMLElement)

describe("Repositories page", () => {
  it("sums up the repositories in the header", async () => {
    mockApi(authedRoutes())
    renderApp("/repositories")
    expect(await screen.findByText("2 configured, 1 running, 1 paused")).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Add repository" })).toBeEnabled()
  })

  it("lists every repository with the week's success rate and labels", async () => {
    mockApi(authedRoutes())
    renderApp("/repositories")
    const table = within(await screen.findByRole("region", { name: "Configured repositories" }))
    expect(table.getByRole("columnheader", { name: "7 days" })).toBeInTheDocument()
    await waitFor(() => expect(row("darkmem").getByText("50%")).toBeInTheDocument())
    expect(row("darkmem").getByText("gpu")).toBeInTheDocument()
    expect(row("old-repo").getByText("Removing. Running jobs finish first.")).toBeInTheDocument()
  })

  it("asks for the last 24 hours of activity", async () => {
    const { calls } = mockApi(authedRoutes())
    renderApp("/repositories")
    await screen.findByRole("region", { name: "Configured repositories" })
    await waitFor(() => expect(calls.some((c) => c.path === "/api/activity" && c.search === "?window=24h&tz=UTC")).toBe(true))
  })

  it("waits for the daemon before the first status", async () => {
    mockApi(authedRoutes({ "GET /api/status": () => json({ error: "connection refused" }, 502) }))
    renderApp("/repositories")
    expect(await screen.findByText("Waiting for the daemon")).toBeInTheDocument()
  })

  it("keeps the table but locks its actions while the daemon is unreachable", { timeout: 10_000 }, async () => {
    let reads = 0
    mockApi(authedRoutes({ "GET /api/status": () => (++reads === 1 ? fixtures.status : json({ error: "connection refused" }, 502)) }))
    const { user } = renderApp("/repositories")
    expect(await screen.findByText("Reconnecting", {}, { timeout: 4000 })).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Add repository" })).toBeDisabled()
    await user.click(screen.getByRole("button", { name: "More actions for darkmem" }))
    expect(await screen.findByRole("menuitem", { name: "Pause" })).toHaveAttribute("aria-disabled", "true")
  })

  it("says when no repository is configured, with the button that fixes it", async () => {
    mockApi(authedRoutes({ "GET /api/status": { ...fixtures.status, repos: [] } }))
    renderApp("/repositories")
    expect(await screen.findByText("No repositories yet")).toBeInTheDocument()
    expect(screen.getAllByRole("button", { name: "Add repository" })).toHaveLength(2)
    expect(screen.queryByRole("region", { name: "Configured repositories" })).toBeNull()
  })

  it("pauses a repository from its row menu", async () => {
    const { calls } = mockApi(authedRoutes({ "POST /api/repos/darkmem/pause": () => noContent() }))
    const { user } = renderApp("/repositories")
    await user.click(await screen.findByRole("button", { name: "More actions for darkmem" }))
    await user.click(await screen.findByRole("menuitem", { name: "Pause" }))
    expect((await screen.findAllByText("Paused darkmem")).length).toBeGreaterThan(0)
    expect(calls.filter((c) => c.method === "POST").map((c) => c.path)).toEqual(["/api/repos/darkmem/pause"])
  })

  it("removes a repository only after confirmation", async () => {
    const { calls } = mockApi(authedRoutes({ "DELETE /api/repos/darkmem": () => noContent() }))
    const { user } = renderApp("/repositories")
    await user.click(await screen.findByRole("button", { name: "More actions for darkmem" }))
    await user.click(await screen.findByRole("menuitem", { name: "Remove" }))
    const ask = within(await screen.findByRole("alertdialog"))
    expect(ask.getByText("Remove darkmem?")).toBeInTheDocument()
    expect(ask.getByText("Its running jobs finish first.")).toBeInTheDocument()
    await user.click(ask.getByRole("button", { name: "Cancel" }))
    await waitFor(() => expect(screen.queryByRole("alertdialog")).toBeNull())
    expect(calls.some((c) => c.method === "DELETE")).toBe(false)

    await user.click(screen.getByRole("button", { name: "More actions for darkmem" }))
    await user.click(await screen.findByRole("menuitem", { name: "Remove" }))
    await user.click(within(await screen.findByRole("alertdialog")).getByRole("button", { name: "Remove" }))
    await waitFor(() => expect(calls.some((c) => c.method === "DELETE" && c.path === "/api/repos/darkmem")).toBe(true))
    expect((await screen.findAllByText("Removing darkmem")).length).toBeGreaterThan(0)
  })

  it("opens a repository from its row", async () => {
    mockApi(authedRoutes())
    const { user, router } = renderApp("/repositories")
    await user.click(await screen.findByRole("link", { name: "darkcloud" }))
    await waitFor(() => expect(router.state.location.pathname).toBe("/repositories/darkcloud"))
  })

  it("shows warm in all mode", async () => {
    mockApi(authedRoutes({ "GET /api/config": { ...fixtures.config, mode: "all" } }))
    renderApp("/repositories")
    expect(await screen.findByRole("columnheader", { name: "Warm" })).toBeInTheDocument()
  })
})
