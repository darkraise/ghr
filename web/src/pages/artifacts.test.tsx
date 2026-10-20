import { screen, waitFor, within } from "@testing-library/react"
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
    expect(screen.getByRole("tab", { name: "GitHub 3" })).toHaveAttribute("aria-selected", "true")
    expect(screen.getByRole("tab", { name: "Self-hosted 5" })).toHaveAttribute("aria-selected", "false")
    expect(within(await screen.findByRole("region", { name: "Recent deletions" })).queryByRole("columnheader", { name: "Store" })).toBeNull()
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

const SELF_HOSTED = "Checked 14:00 · cleanup 7d (1 repository override) · 2 repositories on"

describe("Artifacts page, Self-hosted tab", () => {
  it("opens from the address with the store's summary, cards and policy link", async () => {
    mockApi(authedRoutes())
    renderApp("/artifacts?store=self-hosted")
    expect(await screen.findByText(SELF_HOSTED)).toBeInTheDocument()
    expect(screen.getByRole("tab", { name: "Self-hosted 5" })).toHaveAttribute("aria-selected", "true")
    expect(screen.getByRole("region", { name: "RustFS storage" })).toBeInTheDocument()
    expect(screen.getByRole("switch", { name: "Use RustFS for darkmem" })).toBeChecked()
    expect(screen.getByRole("link", { name: "Policy" })).toHaveAttribute("href", "/settings/self-hosted-storage")
    expect(screen.queryByRole("region", { name: "Quota" })).toBeNull()
  })

  it("puts the chosen tab in the address and back", async () => {
    mockApi(authedRoutes())
    const { user, router } = renderApp("/artifacts")
    await user.click(await screen.findByRole("tab", { name: "Self-hosted 5" }))
    await waitFor(() => expect(router.state.location.search).toEqual({ store: "self-hosted" }))
    expect(await screen.findByText(SELF_HOSTED)).toBeInTheDocument()
    await user.click(screen.getByRole("tab", { name: "GitHub 3" }))
    await waitFor(() => expect(router.state.location.search).toEqual({}))
    expect(await screen.findByText(/^Checked 14:00 · cleanup 2d/)).toBeInTheDocument()
  })

  it("drops a store it does not know from the page and the address", async () => {
    mockApi(authedRoutes())
    const { router } = renderApp("/artifacts?store=bogus")
    expect(await screen.findByRole("tab", { name: "GitHub 3" })).toHaveAttribute("aria-selected", "true")
    await waitFor(() => expect(router.state.location.search).toEqual({}))
  })

  it("starts a check of the self-hosted store", async () => {
    const { calls } = mockApi(authedRoutes({ "POST /api/artifact-store/refresh": () => noContent() }))
    const { user } = renderApp("/artifacts?store=self-hosted")
    await screen.findByText(SELF_HOSTED)
    await user.click(screen.getByRole("button", { name: "Check now" }))
    expect((await screen.findAllByText("Check started")).length).toBeGreaterThan(0)
    expect(calls.filter((c) => c.method === "POST").map((c) => c.path)).toEqual(["/api/artifact-store/refresh"])
  })

  it("explains the feature before it is set up", async () => {
    const empty = { configured: false, used_bytes: 0, count: 0, repos: [], deletions: [] }
    mockApi(authedRoutes({ "GET /api/artifact-store": empty }))
    renderApp("/artifacts?store=self-hosted")
    expect(await screen.findByText("Not set up")).toBeInTheDocument()
    expect(screen.getByRole("tab", { name: "Self-hosted" })).toHaveAttribute("aria-selected", "true")
    expect(screen.getByRole("region", { name: "Keep artifacts on your own S3 storage" })).toBeInTheDocument()
    expect(screen.getByRole("link", { name: "Set up" })).toHaveAttribute("href", "/settings/self-hosted-storage")
    expect(screen.getByRole("button", { name: "Check now" })).toBeDisabled()
    expect(screen.queryByRole("region", { name: "Recent deletions" })).toBeNull()
  })

  it("shows a listing error above the last good listing", async () => {
    mockApi(authedRoutes({ "GET /api/artifact-store": { ...fixtures.artifactStore, error: "RustFS refused the admin key: AccessDenied" } }))
    renderApp("/artifacts?store=self-hosted")
    expect(await screen.findByRole("alert")).toHaveTextContent("Could not list the bucket: RustFS refused the admin key: AccessDenied")
    expect(screen.getByRole("region", { name: "RustFS storage" })).toHaveTextContent("Listed 14:00")
  })

  it("shows a failed read with a retry", async () => {
    let fail = true
    mockApi(authedRoutes({ "GET /api/artifact-store": () => (fail ? json({ error: "daemon busy" }, 500) : fixtures.artifactStore) }))
    const { user } = renderApp("/artifacts?store=self-hosted")
    expect(await screen.findByRole("alert")).toHaveTextContent("daemon busy")
    fail = false
    await user.click(screen.getByRole("button", { name: "Try again" }))
    expect(await screen.findByText(SELF_HOSTED)).toBeInTheDocument()
  })

  it("lists both stores' deletions, newest first, with their store", async () => {
    mockApi(authedRoutes())
    renderApp("/artifacts?store=self-hosted")
    const region = await screen.findByRole("region", { name: "Recent deletions" })
    const rows = within(region).getAllByRole("row").slice(1)
    expect(rows.map((r) => [r.querySelector("td")?.textContent, r.querySelectorAll("td")[1]?.textContent])).toEqual([
      ["13:58", "RustFS"],
      ["13:56", "GitHub"],
      ["12:05", "GitHub"],
      ["11:05", "RustFS"],
    ])
  })
})
