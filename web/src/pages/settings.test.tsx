import { screen, waitFor, within } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import type { Config } from "@/api/types"
import { json, mockApi, noContent } from "@/test/api"
import { authedRoutes, fixtures } from "@/test/fixtures"
import { setViewport } from "@/test/media"
import { renderApp } from "@/test/render"

function routes(over: Record<string, unknown> = {}) {
  return authedRoutes({ "GET /api/token": fixtures.token, ...over })
}

// A daemon that applies each PATCH /config to the config it then serves.
function applyingDaemon() {
  let config: Config = fixtures.config
  return routes({
    "GET /api/config": () => config,
    "PATCH /api/config": ({ body }: { body: unknown }) => {
      const { runner_limits, ...rest } = body as Partial<Config>
      config = { ...config, ...rest, runner_limits: { ...config.runner_limits, ...runner_limits } }
      return noContent()
    },
  })
}

describe("Settings page", () => {
  it("shows the loaded settings", async () => {
    mockApi(routes())
    renderApp("/settings")
    expect(await screen.findByLabelText("Poll interval")).toHaveValue("10s")
    expect(screen.getByLabelText("Start timeout")).toHaveValue("2m0s")
    expect(screen.getByLabelText("Global max")).toHaveValue(2)
    expect(screen.getByLabelText("Disk high-water")).toHaveValue(80)
    expect(screen.getByLabelText("Memory max")).toHaveValue("6G")
    expect(screen.getByText("darkraise")).toBeInTheDocument()
    expect(screen.getByText("homelab")).toBeInTheDocument()
    expect(screen.getByText("Start runners only for queued jobs, up to the global max")).toBeInTheDocument()
    expect(screen.getByText("Limits apply to newly started runners.")).toBeInTheDocument()
    expect(screen.queryByText(/unsaved change/)).toBeNull()
  })

  it("saves only the changed fields", async () => {
    const { calls } = mockApi(applyingDaemon())
    const { user } = renderApp("/settings")
    const poll = await screen.findByLabelText("Poll interval")
    await user.clear(poll)
    await user.type(poll, "15s")
    await user.clear(screen.getByLabelText("Global max"))
    await user.type(screen.getByLabelText("Global max"), "3")
    await user.clear(screen.getByLabelText("Memory max"))
    await user.type(screen.getByLabelText("Memory max"), "8G")
    await user.type(screen.getByRole("textbox", { name: "Global labels" }), "gpu{Enter}")
    expect(screen.getByText("4 unsaved changes")).toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: "Save changes" }))
    await waitFor(() => expect(calls.some((c) => c.method === "PATCH")).toBe(true))
    expect(calls.find((c) => c.method === "PATCH")?.body).toEqual({
      global_max: 3,
      poll_interval: "15s",
      labels: ["homelab", "gpu"],
      runner_limits: { memory_max: "8G" },
    })
    expect((await screen.findAllByText("Settings saved")).length).toBeGreaterThan(0)
    await waitFor(() => expect(screen.queryByText(/unsaved change/)).toBeNull())
    expect(screen.getByLabelText("Poll interval")).toHaveValue("15s")
    expect(screen.queryByText(/Daemon did not apply/)).toBeNull()
  })

  it("disables the fields while the daemon is unreachable", async () => {
    mockApi(routes({ "GET /api/status": () => json({ error: "connection refused" }, 502) }))
    renderApp("/settings")
    await waitFor(() => expect(screen.getByLabelText("Poll interval")).toBeDisabled())
    expect(screen.getByLabelText("Global max")).toBeDisabled()
  })

  it("switches the mode", async () => {
    const { calls } = mockApi(applyingDaemon())
    const { user } = renderApp("/settings")
    await screen.findByLabelText("Poll interval")
    await user.click(screen.getByRole("combobox", { name: "Mode" }))
    await user.click(await screen.findByRole("option", { name: "all" }))
    expect(screen.getByText("Keep warm runners per repo, up to each repo's max")).toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: "Save changes" }))
    await waitFor(() => expect(calls.find((c) => c.method === "PATCH")?.body).toEqual({ mode: "all" }))
  })

  it("does not count an equal duration as a change", async () => {
    mockApi(routes())
    const { user } = renderApp("/settings")
    const start = await screen.findByLabelText("Start timeout")
    await user.clear(start)
    await user.type(start, "120s")
    expect(screen.queryByText(/unsaved change/)).toBeNull()
  })

  it("checks the fields before sending", async () => {
    const { calls } = mockApi(routes())
    const { user } = renderApp("/settings")
    const poll = await screen.findByLabelText("Poll interval")
    await user.clear(poll)
    await user.type(poll, "2s")
    expect(screen.getByText("poll_interval must be at least 5s")).toBeInTheDocument()
    await user.clear(screen.getByLabelText("Global max"))
    expect(screen.getByText("global_max must be >= 1")).toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: "Save changes" }))
    expect((await screen.findAllByText("Fix the highlighted settings first")).length).toBeGreaterThan(0)
    expect(calls.some((c) => c.method === "PATCH")).toBe(false)
  })

  it("lists the daemon's reasons when it rejects a save, and keeps the edits", async () => {
    mockApi(
      routes({
        "PATCH /api/config": () =>
          json(
            { error: "build_cache_keep must look like 20GB; runner_limits.cpu_quota must be a positive percentage such as 200%" },
            400,
          ),
      }),
    )
    const { user } = renderApp("/settings")
    const keep = await screen.findByLabelText("Build cache keep")
    await user.clear(keep)
    await user.type(keep, "lots")
    await user.click(screen.getByRole("button", { name: "Save changes" }))
    expect(await screen.findByText("Save rejected")).toBeInTheDocument()
    expect(screen.getByText("build_cache_keep must look like 20GB")).toBeInTheDocument()
    expect(screen.getByText("runner_limits.cpu_quota must be a positive percentage such as 200%")).toBeInTheDocument()
    expect((await screen.findAllByText("Settings not saved")).length).toBeGreaterThan(0)
    expect(screen.getByLabelText("Build cache keep")).toHaveValue("lots")
  })

  it("names a failed save that is not a rejection", async () => {
    mockApi(routes({ "PATCH /api/config": () => json({ error: "ghr is shutting down" }, 503) }))
    const { user } = renderApp("/settings")
    const max = await screen.findByLabelText("Global max")
    await user.clear(max)
    await user.type(max, "3")
    await user.click(screen.getByRole("button", { name: "Save changes" }))
    expect((await screen.findAllByText("Settings not saved: ghr is shutting down")).length).toBeGreaterThan(0)
    expect(screen.queryByText("Save rejected")).toBeNull()
  })

  it("does not call a trimmed text field unapplied", async () => {
    mockApi(applyingDaemon())
    const { user } = renderApp("/settings")
    const memory = await screen.findByLabelText("Memory max")
    await user.clear(memory)
    await user.type(memory, "8G ")
    await user.click(screen.getByRole("button", { name: "Save changes" }))
    expect((await screen.findAllByText("Settings saved")).length).toBeGreaterThan(0)
    await waitFor(() => expect(screen.queryByText(/unsaved change/)).toBeNull())
    expect(screen.queryByText(/Daemon did not apply/)).toBeNull()
  })

  it("says when the daemon did not apply a saved field", async () => {
    mockApi(routes({ "PATCH /api/config": () => noContent() }))
    const { user } = renderApp("/settings")
    const max = await screen.findByLabelText("Global max")
    await user.clear(max)
    await user.type(max, "3")
    await user.click(screen.getByRole("button", { name: "Save changes" }))
    expect((await screen.findAllByText("Daemon did not apply global_max; is it older than this ghr?")).length).toBeGreaterThan(0)
  })

  it("does not count a space around a loaded value as a change", async () => {
    mockApi(routes())
    const { user } = renderApp("/settings")
    const memory = await screen.findByLabelText("Memory max")
    await user.type(memory, " ")
    expect(screen.queryByText(/unsaved change/)).toBeNull()
  })

  it("shows the saved values when the config cannot be read back after a save", async () => {
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
    const { user } = renderApp("/settings")
    const poll = await screen.findByLabelText("Poll interval")
    await user.clear(poll)
    await user.type(poll, "15s")
    await user.click(screen.getByRole("button", { name: "Save changes" }))
    expect((await screen.findAllByText("Saved, but re-reading the config failed: ghr is restarting")).length).toBeGreaterThan(0)
    expect(screen.getByLabelText("Poll interval")).toHaveValue("15s")
    expect(screen.queryByText(/unsaved change/)).toBeNull()
  })

  it("lets a later read replace the saved values when the read back failed", async () => {
    let reads = 0
    const outside = { ...fixtures.config, poll_interval: "45s" }
    mockApi(
      routes({
        "GET /api/config": () => {
          reads++
          if (reads === 1) return fixtures.config
          return reads === 2 ? json({ error: "ghr is restarting" }, 503) : outside
        },
        "PATCH /api/config": () => noContent(),
      }),
    )
    const { user, queryClient } = renderApp("/settings")
    const poll = await screen.findByLabelText("Poll interval")
    await user.clear(poll)
    await user.type(poll, "15s")
    await user.click(screen.getByRole("button", { name: "Save changes" }))
    expect((await screen.findAllByText(/re-reading the config failed/)).length).toBeGreaterThan(0)
    await queryClient.refetchQueries({ queryKey: ["config"] })
    await waitFor(() => expect(screen.getByLabelText("Poll interval")).toHaveValue("45s"))
    expect(screen.queryByText(/unsaved change/)).toBeNull()
  })

  it("names only the first field the daemon did not apply", async () => {
    mockApi(routes({ "PATCH /api/config": () => noContent() }))
    const { user } = renderApp("/settings")
    const max = await screen.findByLabelText("Global max")
    await user.clear(max)
    await user.type(max, "3")
    const poll = screen.getByLabelText("Poll interval")
    await user.clear(poll)
    await user.type(poll, "15s")
    await user.click(screen.getByRole("button", { name: "Save changes" }))
    expect((await screen.findAllByText("Daemon did not apply global_max; is it older than this ghr?")).length).toBeGreaterThan(0)
    expect(screen.queryByText(/Daemon did not apply poll_interval/)).toBeNull()
  })

  it("discards the edits", async () => {
    mockApi(routes())
    const { user } = renderApp("/settings")
    const poll = await screen.findByLabelText("Poll interval")
    await user.clear(poll)
    await user.type(poll, "15s")
    await user.click(screen.getByRole("button", { name: "Discard" }))
    expect(screen.getByLabelText("Poll interval")).toHaveValue("10s")
    expect(screen.queryByText(/unsaved change/)).toBeNull()
  })

  it("asks before leaving with unsaved changes", async () => {
    mockApi(routes())
    const { user, router } = renderApp("/settings")
    const poll = await screen.findByLabelText("Poll interval")
    await user.clear(poll)
    await user.type(poll, "15s")
    await user.click(screen.getAllByRole("link", { name: "Dashboard" })[0] as HTMLElement)
    expect(await screen.findByText("You have 1 unsaved change on the Settings page.")).toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: "Stay" }))
    expect(router.state.location.pathname).toBe("/settings")
  })

  it("groups the fields into sections and marks a changed one", async () => {
    mockApi(routes())
    const { user } = renderApp("/settings")
    const poll = await screen.findByLabelText("Poll interval")
    for (const name of ["General", "Timing", "Disk and retention", "Runner defaults"]) {
      expect(screen.getByRole("region", { name })).toBeInTheDocument()
    }
    expect(screen.getByText("How often GitHub is checked (at least 5s)")).toHaveClass("text-muted-foreground")
    expect(screen.getByText("Build cache kept when pruning. For example 20GB")).toBeInTheDocument()
    expect(screen.getByText("Change it in config.yaml and restart the daemon")).toBeInTheDocument()
    expect(screen.getByText("darkraise")).toHaveClass("font-mono")
    await user.clear(poll)
    await user.type(poll, "15s")
    expect(within(screen.getByRole("region", { name: "Timing" })).getByText("Changed")).toHaveClass("text-warning")
    expect(screen.queryByText("●")).toBeNull()
  })
})

describe("Settings page layout", () => {
  it("sums up the config in the header", async () => {
    mockApi(routes())
    renderApp("/settings")
    expect(await screen.findByText("Serving darkraise in queue mode")).toBeInTheDocument()
  })

  it("indexes every section at 1280px", async () => {
    setViewport(1280)
    mockApi(routes())
    renderApp("/settings")
    const nav = within(await screen.findByRole("navigation", { name: "Sections" }))
    expect(nav.getAllByRole("button").map((b) => b.textContent)).toEqual([
      "General",
      "Timing",
      "Disk and retention",
      "Runner defaults",
      "GitHub token",
      "Runner and config",
      "Account",
    ])
  })

  it("counts only config fields in the save bar", async () => {
    mockApi(routes())
    const { user } = renderApp("/settings")
    await user.type(await screen.findByLabelText("Current password"), "old password 1")
    expect(screen.queryByText(/unsaved change/)).toBeNull()
  })
})

describe("Account section", () => {
  async function fill(user: ReturnType<typeof renderApp>["user"], current: string, next: string, again: string) {
    await user.type(await screen.findByLabelText("Current password"), current)
    await user.type(screen.getByLabelText("New password"), next)
    await user.type(screen.getByLabelText("Confirm new password"), again)
    await user.click(screen.getByRole("button", { name: "Change password" }))
  }

  it("changes the password", async () => {
    const { calls } = mockApi(routes({ "POST /auth/password": () => noContent() }))
    const { user } = renderApp("/settings")
    await fill(user, "old password 1", "new password 12", "new password 12")
    await waitFor(() => expect(calls.some((c) => c.path === "/auth/password")).toBe(true))
    expect(calls.find((c) => c.path === "/auth/password")?.body).toEqual({ current: "old password 1", new: "new password 12" })
    expect((await screen.findAllByText("Password changed")).length).toBeGreaterThan(0)
    expect(screen.getByLabelText("Current password")).toHaveValue("")
  })

  it("checks the new password before sending", async () => {
    const { calls } = mockApi(routes())
    const { user } = renderApp("/settings")
    await fill(user, "old password 1", "short", "short")
    expect(await screen.findByText("Password must be 12 to 1024 bytes")).toBeInTheDocument()
    await user.clear(screen.getByLabelText("New password"))
    await user.type(screen.getByLabelText("New password"), "new password 12")
    await user.click(screen.getByRole("button", { name: "Change password" }))
    expect(await screen.findByText("The passwords do not match")).toBeInTheDocument()
    expect(calls.some((c) => c.path === "/auth/password")).toBe(false)
  })

  it("says when the current password is wrong", async () => {
    mockApi(routes({ "POST /auth/password": () => json({ error: "wrong password" }, 401) }))
    const { user, router } = renderApp("/settings")
    await fill(user, "bad password 1", "new password 12", "new password 12")
    expect(await screen.findByRole("alert")).toHaveTextContent("The current password is wrong")
    expect(router.state.location.pathname).toBe("/settings")
  })

  it("leaves Log out to the shell, which leaves without asking about unsaved edits", async () => {
    let authenticated = true
    mockApi(
      routes({
        "GET /auth/state": () => ({ setup_required: false, authenticated }),
        "POST /auth/logout": () => {
          authenticated = false
          return noContent()
        },
      }),
    )
    const { user, router } = renderApp("/settings")
    const account = within(await screen.findByRole("region", { name: "Account" }))
    expect(account.queryByRole("button", { name: "Log out" })).toBeNull()
    const poll = await screen.findByLabelText("Poll interval")
    await user.clear(poll)
    await user.type(poll, "15s")
    await user.click(screen.getByRole("button", { name: "Log out" }))
    await waitFor(() => expect(router.state.location.pathname).toBe("/login"))
    expect(screen.queryByText("Unsaved changes")).toBeNull()
  })
})
