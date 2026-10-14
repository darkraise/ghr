import { screen, waitFor, within } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import type { Config } from "@/api/types"
import { json, mockApi, noContent } from "@/test/api"
import { authedRoutes, fixtures } from "@/test/fixtures"
import { renderApp } from "@/test/render"

function routes(over: Record<string, unknown> = {}) {
  return authedRoutes({ "GET /api/token": fixtures.token, ...over })
}

// A daemon that applies each PATCH /config to the config it then serves.
function applyingDaemon(initial: Config = fixtures.config) {
  let config = initial
  return routes({
    "GET /api/config": () => config,
    "PATCH /api/config": ({ body }: { body: unknown }) => {
      const { runner_limits, web, ...rest } = body as Partial<Config>
      config = { ...config, ...rest, runner_limits: { ...config.runner_limits, ...runner_limits }, web: { ...config.web, ...web } }
      return noContent()
    },
  })
}

describe("Settings page", () => {
  it("shows the loaded settings", async () => {
    mockApi(routes())
    renderApp("/settings")
    expect(await screen.findByLabelText("Poll interval")).toHaveValue("10")
    expect(screen.getByLabelText("Start timeout")).toHaveValue("120")
    expect(screen.getByLabelText("Max concurrent runners")).toHaveValue("2")
    expect(screen.getByLabelText("Disk high-water")).toHaveValue("80")
    expect(screen.getByLabelText("Build cache keep")).toHaveValue("20")
    expect(screen.getByLabelText("History retention")).toHaveValue("30")
    expect(screen.getByLabelText("Memory max")).toHaveValue("6144")
    expect(screen.getByLabelText("CPU quota")).toHaveValue("200")
    expect(screen.getByText("MB")).toBeInTheDocument()
    expect(screen.getByText("darkraise")).toBeInTheDocument()
    expect(screen.getByText("homelab")).toBeInTheDocument()
    expect(screen.getByText("Start runners only for queued jobs, up to Max concurrent runners")).toBeInTheDocument()
    expect(screen.getByText("Limits apply to newly started runners.")).toBeInTheDocument()
    expect(screen.queryByText(/unsaved change/)).toBeNull()
  })

  it("saves only the changed fields", { timeout: 15_000 }, async () => {
    const { calls } = mockApi(applyingDaemon())
    const { user } = renderApp("/settings")
    const poll = await screen.findByLabelText("Poll interval")
    await user.clear(poll)
    await user.type(poll, "15")
    await user.clear(screen.getByLabelText("Max concurrent runners"))
    await user.type(screen.getByLabelText("Max concurrent runners"), "3")
    await user.clear(screen.getByLabelText("Memory max"))
    await user.type(screen.getByLabelText("Memory max"), "8192")
    await user.type(screen.getByRole("textbox", { name: "Global labels" }), "gpu{Enter}")
    expect(screen.getByText("4 unsaved changes")).toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: "Save changes" }))
    await waitFor(() => expect(calls.some((c) => c.method === "PATCH")).toBe(true))
    expect(calls.find((c) => c.method === "PATCH")?.body).toEqual({
      global_max: 3,
      poll_interval: "15s",
      labels: ["homelab", "gpu"],
      runner_limits: { memory_max: "8192M" },
    })
    expect((await screen.findAllByText("Settings saved")).length).toBeGreaterThan(0)
    await waitFor(() => expect(screen.queryByText(/unsaved change/)).toBeNull())
    expect(screen.getByLabelText("Poll interval")).toHaveValue("15")
    expect(screen.queryByText(/Daemon did not apply/)).toBeNull()
  })

  it("disables the fields while the daemon is unreachable", async () => {
    mockApi(routes({ "GET /api/status": () => json({ error: "connection refused" }, 502) }))
    renderApp("/settings")
    await waitFor(() => expect(screen.getByLabelText("Poll interval")).toBeDisabled())
    expect(screen.getByLabelText("Max concurrent runners")).toBeDisabled()
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

  it("does not count a retyped value as a change", async () => {
    mockApi(routes())
    const { user } = renderApp("/settings")
    const start = await screen.findByLabelText("Start timeout")
    await user.clear(start)
    await user.type(start, "120")
    expect(screen.queryByText(/unsaved change/)).toBeNull()
  })

  it("checks the fields before sending", async () => {
    const { calls } = mockApi(routes())
    const { user } = renderApp("/settings")
    const poll = await screen.findByLabelText("Poll interval")
    await user.clear(poll)
    await user.type(poll, "2")
    expect(screen.getByText("Enter at least 5 seconds")).toBeInTheDocument()
    const globalMax = screen.getByLabelText("Max concurrent runners")
    await user.clear(globalMax)
    await user.type(globalMax, "100")
    expect(screen.getByText("Enter a whole number from 1 to 99")).toBeInTheDocument()
    await user.clear(globalMax)
    await user.type(globalMax, "99")
    expect(screen.queryByText("Enter a whole number from 1 to 99")).toBeNull()
    await user.clear(globalMax)
    expect(screen.getByText("Enter a whole number from 1 to 99")).toBeInTheDocument()
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
    await user.type(keep, "500")
    await user.click(screen.getByRole("button", { name: "Save changes" }))
    expect(await screen.findByText("Save rejected")).toBeInTheDocument()
    expect(screen.getByText("build_cache_keep must look like 20GB")).toBeInTheDocument()
    expect(screen.getByText("runner_limits.cpu_quota must be a positive percentage such as 200%")).toBeInTheDocument()
    expect((await screen.findAllByText("Settings not saved")).length).toBeGreaterThan(0)
    expect(screen.getByLabelText("Build cache keep")).toHaveValue("500")
  })

  it("names a failed save that is not a rejection", async () => {
    mockApi(routes({ "PATCH /api/config": () => json({ error: "ghr is shutting down" }, 503) }))
    const { user } = renderApp("/settings")
    const max = await screen.findByLabelText("Max concurrent runners")
    await user.clear(max)
    await user.type(max, "3")
    await user.click(screen.getByRole("button", { name: "Save changes" }))
    expect((await screen.findAllByText("Settings not saved: ghr is shutting down")).length).toBeGreaterThan(0)
    expect(screen.queryByText("Save rejected")).toBeNull()
  })

  it("saves an empty memory max as no limit", async () => {
    const { calls } = mockApi(applyingDaemon())
    const { user } = renderApp("/settings")
    const memory = await screen.findByLabelText("Memory max")
    await user.clear(memory)
    expect(memory).toHaveAttribute("placeholder", "No limit")
    await user.click(screen.getByRole("button", { name: "Save changes" }))
    await waitFor(() => expect(calls.find((c) => c.method === "PATCH")?.body).toEqual({ runner_limits: { memory_max: "infinity" } }))
    await waitFor(() => expect(screen.queryByText(/unsaved change/)).toBeNull())
    expect(screen.queryByText(/Daemon did not apply/)).toBeNull()
  })

  it("says when the daemon did not apply a saved field", async () => {
    mockApi(routes({ "PATCH /api/config": () => noContent() }))
    const { user } = renderApp("/settings")
    const max = await screen.findByLabelText("Max concurrent runners")
    await user.clear(max)
    await user.type(max, "3")
    await user.click(screen.getByRole("button", { name: "Save changes" }))
    expect((await screen.findAllByText("Daemon did not apply global_max; is it older than this ghr?")).length).toBeGreaterThan(0)
  })

  it("leaves a value the unit cannot show as it is", async () => {
    const config = { ...fixtures.config, runner_limits: { ...fixtures.config.runner_limits, memory_max: "50%" } }
    const { calls } = mockApi(applyingDaemon(config))
    const { user } = renderApp("/settings")
    const memory = await screen.findByLabelText("Memory max")
    expect(memory).toHaveValue("")
    expect(memory).toHaveAttribute("placeholder", "50%")
    const cpu = screen.getByLabelText("CPU quota")
    await user.clear(cpu)
    await user.type(cpu, "300")
    await user.click(screen.getByRole("button", { name: "Save changes" }))
    await waitFor(() => expect(calls.find((c) => c.method === "PATCH")?.body).toEqual({ runner_limits: { cpu_quota: "300%" } }))
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
    await user.type(poll, "15")
    await user.click(screen.getByRole("button", { name: "Save changes" }))
    expect((await screen.findAllByText("Saved, but re-reading the config failed: ghr is restarting")).length).toBeGreaterThan(0)
    expect(screen.getByLabelText("Poll interval")).toHaveValue("15")
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
    await user.type(poll, "15")
    await user.click(screen.getByRole("button", { name: "Save changes" }))
    expect((await screen.findAllByText(/re-reading the config failed/)).length).toBeGreaterThan(0)
    await queryClient.refetchQueries({ queryKey: ["config"] })
    await waitFor(() => expect(screen.getByLabelText("Poll interval")).toHaveValue("45"))
    expect(screen.queryByText(/unsaved change/)).toBeNull()
  })

  it("names only the first field the daemon did not apply", async () => {
    mockApi(routes({ "PATCH /api/config": () => noContent() }))
    const { user } = renderApp("/settings")
    const max = await screen.findByLabelText("Max concurrent runners")
    await user.clear(max)
    await user.type(max, "3")
    const poll = screen.getByLabelText("Poll interval")
    await user.clear(poll)
    await user.type(poll, "15")
    await user.click(screen.getByRole("button", { name: "Save changes" }))
    expect((await screen.findAllByText("Daemon did not apply global_max; is it older than this ghr?")).length).toBeGreaterThan(0)
    expect(screen.queryByText(/Daemon did not apply poll_interval/)).toBeNull()
  })

  it("discards the edits", async () => {
    mockApi(routes())
    const { user } = renderApp("/settings")
    const poll = await screen.findByLabelText("Poll interval")
    await user.clear(poll)
    await user.type(poll, "15")
    await user.click(screen.getByRole("button", { name: "Discard" }))
    expect(screen.getByLabelText("Poll interval")).toHaveValue("10")
    expect(screen.queryByText(/unsaved change/)).toBeNull()
  })

  it("asks before leaving with unsaved changes", async () => {
    mockApi(routes())
    const { user, router } = renderApp("/settings")
    const poll = await screen.findByLabelText("Poll interval")
    await user.clear(poll)
    await user.type(poll, "15")
    await user.click(screen.getAllByRole("link", { name: "Dashboard" })[0] as HTMLElement)
    expect(await screen.findByText("You have 1 unsaved change on the Settings page.")).toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: "Stay" }))
    expect(router.state.location.pathname).toBe("/settings")
  })

  it("groups the fields into sections and marks a changed one", async () => {
    mockApi(routes())
    const { user } = renderApp("/settings")
    const poll = await screen.findByLabelText("Poll interval")
    for (const name of ["Scheduling", "Timing", "Disk and retention", "Runner defaults"]) {
      expect(screen.getByRole("region", { name })).toBeInTheDocument()
    }
    expect(screen.getByText("How often GitHub is checked. At least 5 seconds.")).toHaveClass("text-muted-foreground")
    expect(screen.getByText("Build cache kept when pruning")).toBeInTheDocument()
    expect(screen.getByText("Change it in config.yaml and restart the daemon")).toBeInTheDocument()
    expect(screen.getByText("darkraise")).toHaveClass("font-mono")
    await user.clear(poll)
    await user.type(poll, "15")
    expect(within(screen.getByRole("region", { name: "Timing" })).getByText("Changed")).toHaveClass("text-warning")
    expect(screen.queryByText("●")).toBeNull()
  })
})

describe("Settings page layout", () => {
  it("shows a failed config read with a retry instead of loading forever", async () => {
    mockApi(routes({ "GET /api/config": () => json({ error: "config unreadable" }, 500) }))
    renderApp("/settings")
    expect(await screen.findByRole("alert")).toHaveTextContent("config unreadable")
    expect(screen.getByRole("button", { name: "Try again" })).toBeInTheDocument()
    expect(screen.queryByText("Loading")).toBeNull()
  })

  it("sums up the config in the header", async () => {
    mockApi(routes())
    renderApp("/settings")
    expect(await screen.findByText("Serving darkraise in queue mode")).toBeInTheDocument()
  })

  it("counts only config fields in the save bar", async () => {
    mockApi(routes())
    const { user, router } = renderApp("/settings/account")
    await user.type(await screen.findByLabelText("Current password"), "old password 1")
    expect(screen.queryByText(/unsaved change/)).toBeNull()
    expect(screen.queryByRole("button", { name: "Save changes" })).toBeNull()
    await user.click(within(screen.getByRole("navigation", { name: "Settings" })).getByRole("link", { name: "General" }))
    const poll = await screen.findByLabelText("Poll interval")
    expect(router.state.location.pathname).toBe("/settings")
    expect(screen.queryByText("Unsaved changes")).toBeNull()
    await user.clear(poll)
    await user.type(poll, "15")
    expect(screen.getByText("1 unsaved change")).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Save changes" })).toBeInTheDocument()
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
    const { user } = renderApp("/settings/account")
    await fill(user, "old password 1", "new password 12", "new password 12")
    await waitFor(() => expect(calls.some((c) => c.path === "/auth/password")).toBe(true))
    expect(calls.find((c) => c.path === "/auth/password")?.body).toEqual({ current: "old password 1", new: "new password 12" })
    expect((await screen.findAllByText("Password changed")).length).toBeGreaterThan(0)
    expect(screen.getByLabelText("Current password")).toHaveValue("")
  })

  it("checks the new password before sending", async () => {
    const { calls } = mockApi(routes())
    const { user } = renderApp("/settings/account")
    await fill(user, "old password 1", "short", "short")
    expect(await screen.findByText("Password must be 12 to 1024 characters")).toBeInTheDocument()
    await user.clear(screen.getByLabelText("New password"))
    await user.type(screen.getByLabelText("New password"), "new password 12")
    await user.click(screen.getByRole("button", { name: "Change password" }))
    expect(await screen.findByText("The passwords do not match")).toBeInTheDocument()
    expect(calls.some((c) => c.path === "/auth/password")).toBe(false)
  })

  it("says when the current password is wrong", async () => {
    mockApi(routes({ "POST /auth/password": () => json({ error: "wrong password" }, 403) }))
    const { user, router } = renderApp("/settings/account")
    await fill(user, "bad password 1", "new password 12", "new password 12")
    expect(await screen.findByRole("alert")).toHaveTextContent("The current password is wrong")
    expect(router.state.location.pathname).toBe("/settings/account")
  })

  it("says when password changes are throttled", async () => {
    mockApi(
      routes({
        "POST /auth/password": () =>
          json({ error: "too many failed logins; retry after 2026-10-06T14:20:00Z", retry_at: "2026-10-06T14:20:00Z" }, 429),
      }),
    )
    const { user } = renderApp("/settings/account")
    await fill(user, "bad password 1", "new password 12", "new password 12")
    expect(await screen.findByRole("alert")).toHaveTextContent(/^Too many failed attempts; try again after 14:20\.$/)
  })

  it("a 401 on password change goes to the login page", async () => {
    let authenticated = true
    mockApi(
      routes({
        "GET /auth/state": () => ({ setup_required: false, authenticated }),
        "POST /auth/password": () => {
          authenticated = false
          return json({ error: "not logged in" }, 401)
        },
      }),
    )
    const { user, router } = renderApp("/settings/account")
    await fill(user, "old password 1", "new password 12", "new password 12")
    await waitFor(() => expect(router.state.location.pathname).toBe("/login"))
    expect(screen.queryByText("The current password is wrong")).not.toBeInTheDocument()
  })
})

describe("Settings sections", () => {
  it("opens on General with the section list beside it", async () => {
    mockApi(routes())
    renderApp("/settings")
    const nav = await screen.findByRole("navigation", { name: "Settings" })
    expect(within(nav).getAllByRole("link").map((a) => a.textContent)).toEqual([
      "General",
      "GitHub token",
      "GitHub storage",
      "Runner software",
      "Account",
      "Toolchains",
      "Storage",
    ])
    expect(within(nav).getByText("Host")).toBeInTheDocument()
    expect(within(nav).getByRole("link", { name: "General" })).toHaveAttribute("aria-current", "page")
    expect(await screen.findByRole("heading", { level: 1, name: "General" })).toBeInTheDocument()
    expect(await screen.findByLabelText("Poll interval")).toBeInTheDocument()
    expect(screen.getAllByRole("heading", { name: "General" })).toHaveLength(1)
    expect(screen.queryByRole("heading", { name: "GitHub token" })).not.toBeInTheDocument()
  })

  it.each([
    ["token", "GitHub token"],
    ["github-storage", "GitHub storage"],
    ["runner", "Runner software"],
    ["account", "Account"],
    ["toolchains", "Toolchains"],
    ["storage", "Storage"],
  ])("shows the %s section at its own address", async (section, title) => {
    mockApi(routes({ "GET /api/storage": fixtures.storage }))
    renderApp(`/settings/${section}`)
    const nav = await screen.findByRole("navigation", { name: "Settings" })
    expect(within(nav).getByRole("link", { name: title })).toHaveAttribute("aria-current", "page")
    expect(within(nav).getByRole("link", { name: "General" })).not.toHaveAttribute("aria-current")
    expect(await screen.findByRole("heading", { level: 1, name: title })).toBeInTheDocument()
    expect(screen.getAllByRole("heading", { name: title })).toHaveLength(1)
    expect(screen.queryByLabelText("Poll interval")).not.toBeInTheDocument()
  })

  it("says so when the section does not exist", async () => {
    mockApi(routes())
    renderApp("/settings/nope")
    expect(await screen.findByText("Settings has no such section.")).toBeInTheDocument()
  })

  it("guards an unsaved change when another section is opened from the list", async () => {
    mockApi(routes())
    const { user, router } = renderApp("/settings")
    const poll = await screen.findByLabelText("Poll interval")
    await user.clear(poll)
    await user.type(poll, "15")
    await user.click(within(screen.getByRole("navigation", { name: "Settings" })).getByRole("link", { name: "GitHub token" }))
    expect(await screen.findByText("Unsaved changes")).toBeInTheDocument()
    expect(router.state.location.pathname).toBe("/settings")
  })

  it("keeps Log out out of the Account section", async () => {
    mockApi(routes())
    renderApp("/settings/account")
    const account = within(await screen.findByRole("region", { name: "Password" }))
    expect(account.queryByRole("button", { name: "Log out" })).toBeNull()
  })
})

describe("Web access", () => {
  const group = () => within(screen.getByRole("region", { name: "Web access" }))
  const reason = "you reached ghr on 192.168.0.99; listening on 172.17.0.1 would lock this browser out"

  it("shows the listen address and the allowed hostnames after Runner defaults", async () => {
    mockApi(routes())
    renderApp("/settings")
    const listen = await screen.findByLabelText("Listen address")
    expect(listen).toHaveValue("0.0.0.0:8080")
    expect(listen).toHaveClass("font-mono")
    expect(group().getByText("Changes apply after a restart.")).toBeInTheDocument()
    expect(group().getByRole("button", { name: "Remove ghr.lan" })).toBeInTheDocument()
    expect(group().getByText("IP addresses and localhost always work.")).toBeInTheDocument()
    expect(screen.queryByText("The web UI turns off at the next restart.")).toBeNull()
    const titles = screen.getAllByRole("region").map((r) => r.querySelector("h2")?.textContent)
    expect(titles.indexOf("Web access")).toBe(titles.indexOf("Runner defaults") + 1)
  })

  it("saves both under web", async () => {
    const { calls } = mockApi(applyingDaemon())
    const { user } = renderApp("/settings")
    const listen = await screen.findByLabelText("Listen address")
    await user.clear(listen)
    await user.type(listen, "0.0.0.0:9090")
    await user.type(screen.getByRole("textbox", { name: "Allowed hostnames" }), "ghr.home{Enter}")
    expect(screen.getByText("2 unsaved changes")).toBeInTheDocument()
    expect(group().getAllByText("Changed")).toHaveLength(2)
    await user.click(screen.getByRole("button", { name: "Save changes" }))
    await waitFor(() =>
      expect(calls.find((c) => c.method === "PATCH")?.body).toEqual({ web: { listen: "0.0.0.0:9090", hosts: ["ghr.lan", "ghr.home"] } }),
    )
    await waitFor(() => expect(screen.queryByText(/unsaved change/)).toBeNull())
    expect(screen.queryByText(/Daemon did not apply web\./)).toBeNull()
  })

  it("checks the address and the hostnames before sending", async () => {
    const { calls } = mockApi(routes())
    const { user } = renderApp("/settings")
    const listen = await screen.findByLabelText("Listen address")
    await user.clear(listen)
    await user.type(listen, "0.0.0.0")
    expect(group().getByText("Enter host:port with a port from 1 to 65535, such as 0.0.0.0:8080")).toBeInTheDocument()
    await user.type(listen, ":70000")
    expect(group().getByText("Enter host:port with a port from 1 to 65535, such as 0.0.0.0:8080")).toBeInTheDocument()
    await user.clear(listen)
    // user-event reads "[" as the start of a key name; "[[" types one.
    await user.type(listen, "[[::]:8081")
    expect(listen).toHaveValue("[::]:8081")
    expect(group().queryByText("Enter host:port with a port from 1 to 65535, such as 0.0.0.0:8080")).toBeNull()
    await user.type(screen.getByRole("textbox", { name: "Allowed hostnames" }), "https://ghr.lan{Enter}")
    expect(group().getByText("https://ghr.lan is not a bare hostname such as ghr.lan")).toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: "Save changes" }))
    expect((await screen.findAllByText("Fix the highlighted settings first")).length).toBeGreaterThan(0)
    expect(calls.some((c) => c.method === "PATCH")).toBe(false)
  })

  it("does not let a saved address it would flag block saving other fields", async () => {
    const { calls } = mockApi(applyingDaemon({ ...fixtures.config, web: { listen: "0.0.0.0:http", hosts: ["ghr.lan"] } }))
    const { user } = renderApp("/settings")
    const poll = await screen.findByLabelText("Poll interval")
    expect(screen.getByLabelText("Listen address")).toHaveValue("0.0.0.0:http")
    expect(group().queryByText("Enter host:port with a port from 1 to 65535, such as 0.0.0.0:8080")).toBeNull()
    await user.clear(poll)
    await user.type(poll, "15")
    await user.click(screen.getByRole("button", { name: "Save changes" }))
    await waitFor(() => expect(calls.find((c) => c.method === "PATCH")?.body).toEqual({ poll_interval: "15s" }))
  })

  it("does not count reordered hostnames, in any case, as a change", async () => {
    mockApi(routes({ "GET /api/config": { ...fixtures.config, web: { listen: "0.0.0.0:8080", hosts: ["ghr.lan", "ghr.home"] } } }))
    const { user } = renderApp("/settings")
    await user.click(await screen.findByRole("button", { name: "Remove ghr.lan" }))
    expect(screen.getByText("1 unsaved change")).toBeInTheDocument()
    await user.type(screen.getByRole("textbox", { name: "Allowed hostnames" }), "GHR.LAN{Enter}")
    expect(group().getByRole("button", { name: "Remove GHR.LAN" })).toBeInTheDocument()
    expect(screen.queryByText(/unsaved change/)).toBeNull()
  })

  it("shows a web.listen refusal under its field, without the key", async () => {
    mockApi(routes({ "PATCH /api/config": () => json({ error: `web.listen: ${reason}` }, 400) }))
    const { user } = renderApp("/settings")
    const listen = await screen.findByLabelText("Listen address")
    await user.clear(listen)
    await user.type(listen, "172.17.0.1:8080")
    await user.click(screen.getByRole("button", { name: "Save changes" }))
    expect(await group().findByText(reason)).toHaveClass("text-destructive")
    expect(screen.queryByText("Save rejected")).toBeNull()
    expect(listen).toHaveValue("172.17.0.1:8080")
  })

  it("shows a web.hosts refusal under its field", async () => {
    mockApi(routes({ "PATCH /api/config": () => json({ error: "web.hosts: you are using ghr.lan; removing it would lock this browser out" }, 400) }))
    const { user } = renderApp("/settings")
    await user.click(await screen.findByRole("button", { name: "Remove ghr.lan" }))
    await user.click(screen.getByRole("button", { name: "Save changes" }))
    expect(await group().findByText("you are using ghr.lan; removing it would lock this browser out")).toBeInTheDocument()
    expect(screen.queryByText("Save rejected")).toBeNull()
  })

  it("keeps any other refusal in the form's alert", async () => {
    mockApi(routes({ "PATCH /api/config": () => json({ error: "web.hosts entries must be bare hostnames such as ghr.lan" }, 400) }))
    const { user } = renderApp("/settings")
    await user.type(await screen.findByRole("textbox", { name: "Allowed hostnames" }), "ghr.home{Enter}")
    await user.click(screen.getByRole("button", { name: "Save changes" }))
    expect(await screen.findByText("Save rejected")).toBeInTheDocument()
    expect(screen.getByText("web.hosts entries must be bare hostnames such as ghr.lan")).toBeInTheDocument()
  })

  it("notes that an empty saved address turns the web UI off", async () => {
    mockApi(routes({ "GET /api/config": { ...fixtures.config, web: { hosts: ["ghr.lan"] } } }))
    renderApp("/settings")
    expect(await screen.findByLabelText("Listen address")).toHaveValue("")
    expect(group().getByText("The web UI turns off at the next restart.")).toBeInTheDocument()
    expect(screen.queryByText(/unsaved change/)).toBeNull()
  })
})
