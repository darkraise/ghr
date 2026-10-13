import { screen, waitFor } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import { json, mockApi, noContent } from "@/test/api"
import { authedRoutes, fixtures } from "@/test/fixtures"
import { renderApp } from "@/test/render"

const SUMMARY = " · cleanup 2d (1 repository override) · warnings at 70% and 90%"

describe("Artifacts page", () => {
  it("is served at /artifacts and says when GitHub was last checked", async () => {
    mockApi(authedRoutes())
    renderApp("/artifacts")
    expect(await screen.findByRole("heading", { level: 1, name: "Artifacts" })).toBeInTheDocument()
    expect(await screen.findByText(`Checked 14:00${SUMMARY}`)).toBeInTheDocument()
  })

  it("says when nothing has been checked yet", async () => {
    mockApi(authedRoutes({ "GET /api/github-storage": { repos: [], deletions: [] } }))
    renderApp("/artifacts")
    expect(await screen.findByText(`Not checked yet${SUMMARY}`)).toBeInTheDocument()
  })

  it("shows a failed read with a retry", async () => {
    let fail = true
    mockApi(authedRoutes({ "GET /api/github-storage": () => (fail ? json({ error: "daemon busy" }, 500) : fixtures.githubStorage) }))
    const { user } = renderApp("/artifacts")
    const alert = await screen.findByRole("alert")
    expect(alert).toHaveTextContent("daemon busy")
    fail = false
    await user.click(screen.getByRole("button", { name: "Try again" }))
    expect(await screen.findByText(`Checked 14:00${SUMMARY}`)).toBeInTheDocument()
    expect(screen.queryByRole("alert")).toBeNull()
  })

  it("shows why the repositories could not be listed", async () => {
    mockApi(authedRoutes({ "GET /api/github-storage": { ...fixtures.githubStorage, error: "GitHub did not answer in time" } }))
    renderApp("/artifacts")
    expect(await screen.findByRole("alert")).toHaveTextContent("Could not list the repositories: GitHub did not answer in time")
  })

  it("starts a check, and shows the refusal while one runs", async () => {
    let busy = false
    const { calls } = mockApi(
      authedRoutes({
        "POST /api/github-storage/refresh": () => (busy ? json({ error: "a GitHub storage check is already running" }, 409) : noContent()),
      }),
    )
    const { user } = renderApp("/artifacts")
    await user.click(await screen.findByRole("button", { name: "Check now" }))
    expect((await screen.findAllByText("Check started")).length).toBeGreaterThan(0)
    busy = true
    await waitFor(() => expect(screen.getByRole("button", { name: "Check now" })).toBeEnabled())
    await user.click(screen.getByRole("button", { name: "Check now" }))
    expect((await screen.findAllByText("a GitHub storage check is already running")).length).toBeGreaterThan(0)
    expect(calls.filter((c) => c.method === "POST")).toHaveLength(2)
  })

  it("sums up a policy without overrides and links to it in Settings, without a policy form", async () => {
    mockApi(authedRoutes({ "GET /api/config": { ...fixtures.config, github_storage: { included_mb: 1024, warn_pct: [60, 85] } } }))
    renderApp("/artifacts")
    expect(await screen.findByText("Checked 14:00 · cleanup off · warnings at 60% and 85%")).toBeInTheDocument()
    expect(screen.getByRole("link", { name: "Policy" })).toHaveAttribute("href", "/settings/github-storage")
    expect(screen.queryByRole("region", { name: "Policy" })).toBeNull()
    expect(screen.queryByRole("spinbutton", { name: "Included storage" })).toBeNull()
  })
})
