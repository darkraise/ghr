import { screen, waitFor } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import type { Config } from "@/api/types"
import { json, mockApi, noContent } from "@/test/api"
import { authedRoutes, fixtures } from "@/test/fixtures"
import { renderApp } from "@/test/render"

function routes(over: Record<string, unknown> = {}) {
  return authedRoutes({ "GET /api/history": [], ...over })
}

// The daemon applies a PATCH, so the config read after it reflects the save.
function applyingRoutes() {
  let config: Config = fixtures.config
  return routes({
    "GET /api/config": () => config,
    "PATCH /api/config": ({ body }: { body: unknown }) => {
      const patch = (body as { repos: Record<string, Record<string, unknown>> }).repos.darkmem
      config = { ...config, repos: (config.repos ?? []).map((r) => (r.name === "darkmem" ? { ...r, ...patch } : r)) }
      return noContent()
    },
  })
}

describe("repository page", () => {
  it("shows the repo's settings and the labels its runners get", async () => {
    mockApi(routes())
    renderApp("/repositories/darkmem")
    expect(await screen.findByLabelText("Max")).toHaveValue(2)
    expect(screen.getByLabelText("Warm")).toHaveValue(1)
    expect(screen.getByRole("button", { name: "Remove gpu" })).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Remove darkmem-" })).toBeInTheDocument()
    expect(screen.getByText("self-hosted linux x64")).toBeInTheDocument()
    expect(screen.getByText("homelab (global) gpu")).toBeInTheDocument()
    expect(screen.getByText("1/2 running · 3 queued")).toBeInTheDocument()
  })

  it("saves only the changed field", async () => {
    const { calls } = mockApi(applyingRoutes())
    const { user } = renderApp("/repositories/darkmem")
    const max = await screen.findByLabelText("Max")
    await user.clear(max)
    await user.type(max, "3")
    expect(screen.getByText("● 1 unsaved change")).toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: "Save changes" }))
    expect((await screen.findAllByText("Repositories saved")).length).toBeGreaterThan(0)
    expect(calls.find((c) => c.method === "PATCH")?.body).toEqual({ repos: { darkmem: { max: 3 } } })
    await waitFor(() => expect(screen.queryByText("● 1 unsaved change")).toBeNull())
    expect(screen.queryByText(/daemon did not apply/)).toBeNull()
  })

  it("starts a fresh draft on another repository", async () => {
    mockApi(routes())
    const { user, router } = renderApp("/repositories/darkmem")
    const max = await screen.findByLabelText("Max")
    await user.clear(max)
    await user.type(max, "2")
    expect(screen.queryByText(/unsaved change/)).toBeNull()
    await router.navigate({ to: "/repositories/$name", params: { name: "darkcloud" } })
    expect(await screen.findByRole("heading", { name: "darkcloud" })).toBeInTheDocument()
    expect(screen.getByLabelText("Max")).toHaveValue(null)
    expect(screen.queryByText(/unsaved change/)).toBeNull()
  })

  it("says when the daemon did not apply a field", async () => {
    mockApi(routes({ "PATCH /api/config": () => noContent() }))
    const { user } = renderApp("/repositories/darkmem")
    await user.type(await screen.findByRole("textbox", { name: "Prefixes" }), "test_{Enter}")
    await user.click(screen.getByRole("button", { name: "Save changes" }))
    expect(
      (await screen.findAllByText("daemon did not apply darkmem.cleanup_name_prefixes; is it older than this ghr?")).length,
    ).toBeGreaterThan(0)
  })

  it("shows the saved value when the config cannot be read back after a save", async () => {
    let saved = false
    mockApi(
      routes({
        "GET /api/config": () => (saved ? json({ error: "ghr is restarting" }, 503) : fixtures.config),
        "PATCH /api/config": () => {
          saved = true
          return noContent()
        },
      }),
    )
    const { user } = renderApp("/repositories/darkmem")
    await user.type(await screen.findByRole("textbox", { name: "Prefixes" }), "test_{Enter}")
    await user.click(screen.getByRole("button", { name: "Save changes" }))
    expect((await screen.findAllByText("saved, but re-reading the config failed: ghr is restarting")).length).toBeGreaterThan(0)
    expect(screen.getByText("test_")).toBeInTheDocument()
    expect(screen.queryByText(/unsaved change/)).toBeNull()
  })

  it("shows a rejected save and keeps the edit", async () => {
    mockApi(
      routes({
        "PATCH /api/config": () =>
          json({ error: "darkmem: needs at least one label in labels or repo labels; a; b; c" }, 400),
      }),
    )
    const { user } = renderApp("/repositories/darkmem")
    await user.click(await screen.findByRole("button", { name: "Remove gpu" }))
    await user.click(screen.getByRole("button", { name: "Save changes" }))
    expect(await screen.findByText("Save rejected")).toBeInTheDocument()
    expect(screen.getByText("✖ darkmem: needs at least one label in labels or repo labels")).toBeInTheDocument()
    expect(screen.getByText("… and 1 more")).toBeInTheDocument()
    expect((await screen.findAllByText("repositories not saved")).length).toBeGreaterThan(0)
    expect(screen.getByText("● 1 unsaved change")).toBeInTheDocument()
  })

  it("blocks a save while warm exceeds max", async () => {
    const { calls } = mockApi(routes())
    const { user } = renderApp("/repositories/darkmem")
    const warm = await screen.findByLabelText("Warm")
    await user.clear(warm)
    await user.type(warm, "3")
    expect(screen.getByText("✖ warm must be <= max")).toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: "Save changes" }))
    expect((await screen.findAllByText("fix the highlighted settings first")).length).toBeGreaterThan(0)
    expect(calls.some((c) => c.method === "PATCH")).toBe(false)
  })

  it("keeps an emptied box empty and refuses to save it", async () => {
    const { calls } = mockApi(routes())
    const { user } = renderApp("/repositories/darkmem")
    const max = await screen.findByLabelText("Max")
    await user.clear(max)
    expect(max).toHaveValue(null)
    expect(screen.getByText("✖ enter a number from 0 to 99")).toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: "Save changes" }))
    expect((await screen.findAllByText("fix the highlighted settings first")).length).toBeGreaterThan(0)
    expect(calls.some((c) => c.method === "PATCH")).toBe(false)
  })

  it("disables the form while the daemon is unreachable", async () => {
    mockApi(routes({ "GET /api/status": () => json({ error: "connection refused" }, 502) }))
    renderApp("/repositories/darkmem")
    await waitFor(() => expect(screen.getByLabelText("Max")).toBeDisabled())
    expect(screen.getByRole("textbox", { name: "Prefixes" })).toBeDisabled()
  })

  it("discards the edits", async () => {
    mockApi(routes())
    const { user } = renderApp("/repositories/darkmem")
    const max = await screen.findByLabelText("Max")
    await user.clear(max)
    await user.type(max, "5")
    await user.click(screen.getByRole("button", { name: "Discard" }))
    expect(screen.getByLabelText("Max")).toHaveValue(2)
    expect(screen.queryByText("● 1 unsaved change")).toBeNull()
  })

  it("asks before leaving with unsaved edits", async () => {
    mockApi(routes())
    const { user } = renderApp("/repositories/darkmem")
    const max = await screen.findByLabelText("Max")
    await user.clear(max)
    await user.type(max, "5")
    await user.click(screen.getByRole("link", { name: /^Runners/ }))
    expect(await screen.findByText("You have 1 unsaved change on the Repositories page.")).toBeInTheDocument()
  })

  it("locks the form while the repo is being removed", async () => {
    const repos = fixtures.status.repos.map((r) => (r.name === "darkmem" ? { ...r, removing: true } : r))
    mockApi(routes({ "GET /api/status": { ...fixtures.status, repos } }))
    renderApp("/repositories/darkmem")
    expect(await screen.findByLabelText("Max")).toBeDisabled()
    expect(screen.getByRole("textbox", { name: "Repo labels" })).toBeDisabled()
  })

  it("says when the repo is not configured", async () => {
    mockApi(routes())
    renderApp("/repositories/nope")
    expect(await screen.findByText("Repository not found")).toBeInTheDocument()
  })

  it("is reached from the Repositories page", async () => {
    mockApi(routes())
    const { user, router } = renderApp("/repositories")
    await user.click(await screen.findByRole("link", { name: "Manage darkmem" }))
    await waitFor(() => expect(router.state.location.pathname).toBe("/repositories/darkmem"))
  })
})
