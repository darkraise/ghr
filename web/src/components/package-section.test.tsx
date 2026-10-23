import { act, screen, waitFor, within } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import { invalidateTopics } from "@/api/use-change-stream"
import { json, mockApi, noContent } from "@/test/api"
import { authedRoutes, fixtures } from "@/test/fixtures"
import { renderApp } from "@/test/render"

const routes = (over: Record<string, unknown> = {}) => authedRoutes(over)

const section = async () => within(await screen.findByRole("region", { name: "System packages" }))

const managedRow = async (name: string) => {
  const table = await screen.findByRole("table", { name: "Managed packages" })
  return within(within(table).getByText(name).closest("tr") as HTMLElement)
}

const packageCalls = (calls: { method: string; path: string }[]) => calls.filter((c) => c.method === "GET" && c.path === "/api/packages").length

describe("System packages section", () => {
  it("lists the managed packages with their version, size, upgrade and error", async () => {
    mockApi(routes())
    renderApp("/toolchains")
    const ripgrep = await managedRow("ripgrep")
    expect(ripgrep.getByText("ripgrep")).toHaveClass("font-mono")
    expect(ripgrep.getByText("14.1.0-1")).toHaveClass("font-mono")
    expect(ripgrep.getByText("5.1 MB")).toHaveClass("font-mono")
    expect(ripgrep.getByText("Upgrade to 14.1.1-1")).toBeInTheDocument()
    const shellcheck = await managedRow("shellcheck")
    expect(shellcheck.queryByText(/^Upgrade to/)).toBeNull()
    const yq = await managedRow("yq")
    expect(yq.getByText("not installed")).toBeInTheDocument()
    expect(yq.getByText("no installation candidate in the configured apt sources")).toHaveClass("text-destructive")
  })

  it("says when the index was last updated, on the daemon's clock", async () => {
    mockApi(routes())
    renderApp("/toolchains")
    const s = await section()
    expect(await s.findByText("3h ago")).toHaveAttribute("dateTime", "2026-10-03T11:05:00Z")
    expect(s.getByText(/^index updated/)).toBeInTheDocument()
  })

  it("shows one line when apt is not available", async () => {
    mockApi(routes({ "GET /api/packages": () => json({ error: "apt is not available on this host" }, 503) }))
    renderApp("/toolchains")
    const s = await section()
    expect(await s.findByText("apt is not available on this host")).toBeInTheDocument()
    expect(s.queryByRole("button", { name: "Add package" })).toBeNull()
    expect(s.queryByRole("alert")).toBeNull()
  })

  it("shows a failed read with a retry", async () => {
    mockApi(routes({ "GET /api/packages": () => json({ error: "dpkg-query failed" }, 500) }))
    renderApp("/toolchains")
    const s = await section()
    expect(await s.findByText("dpkg-query failed")).toBeInTheDocument()
    expect(s.getByRole("button", { name: "Try again" })).toBeInTheDocument()
  })

  it("reads only apt's own 503 as apt not available", async () => {
    mockApi(routes({ "GET /api/packages": () => json({ error: "ghr is shutting down" }, 503) }))
    renderApp("/toolchains")
    const s = await section()
    expect(await s.findByText("ghr is shutting down")).toBeInTheDocument()
    expect(s.getByRole("button", { name: "Try again" })).toBeInTheDocument()
    expect(s.queryByText("apt is not available on this host")).toBeNull()
  })

  it("keeps the loaded list when a refetch fails", async () => {
    let reads = 0
    const { calls } = mockApi(
      routes({ "GET /api/packages": () => (++reads === 1 ? fixtures.packages : json({ error: "dpkg-query failed" }, 500)) }),
    )
    const { queryClient } = renderApp("/toolchains")
    await managedRow("ripgrep")
    act(() => invalidateTopics(queryClient, ["packages"]))
    await waitFor(() => expect(packageCalls(calls)).toBe(2))
    const s = await section()
    expect(await s.findByText("dpkg-query failed")).toBeInTheDocument()
    expect((await managedRow("ripgrep")).getByText("14.1.0-1")).toBeInTheDocument()
  })

  it("says when no package is managed, and offers Upgrade all only when one is upgradable", async () => {
    mockApi(routes({ "GET /api/packages": { ...fixtures.packages, managed: null } }))
    renderApp("/toolchains")
    const s = await section()
    expect(await s.findByText("ghr manages no apt packages yet")).toBeInTheDocument()
    expect(s.getByRole("button", { name: "Add package" })).toBeEnabled()
    expect(s.queryByRole("button", { name: /^Upgrade all/ })).toBeNull()
  })

  it("queues an upgrade of every managed package", async () => {
    const { calls } = mockApi(routes({ "POST /api/packages/upgrade": () => noContent() }))
    const { user } = renderApp("/toolchains")
    await user.click(await (await section()).findByRole("button", { name: "Upgrade all (1)" }))
    await waitFor(() => expect(calls.find((c) => c.path === "/api/packages/upgrade")?.body).toEqual({ all: true }))
    expect((await screen.findAllByText("Queued: upgrade all managed packages")).length).toBeGreaterThan(0)
  })

  it("queues an index refresh", async () => {
    const { calls } = mockApi(routes({ "POST /api/packages/refresh": () => noContent() }))
    const { user } = renderApp("/toolchains")
    await user.click(await (await section()).findByRole("button", { name: "Refresh index" }))
    await waitFor(() => expect(calls.some((c) => c.method === "POST" && c.path === "/api/packages/refresh")).toBe(true))
    expect((await screen.findAllByText("Queued: refresh apt index")).length).toBeGreaterThan(0)
  })

  it("holds the actions while a request is on its way", async () => {
    let release: (() => void) | undefined
    mockApi(
      routes({
        "POST /api/packages/refresh": () =>
          new Promise<Response>((resolve) => {
            release = () => resolve(noContent())
          }),
      }),
    )
    const { user } = renderApp("/toolchains")
    const refresh = await (await section()).findByRole("button", { name: "Refresh index" })
    await user.click(refresh)
    await waitFor(() => expect(refresh).toBeDisabled())
    expect((await managedRow("ripgrep")).getByRole("button", { name: "More actions for ripgrep" })).toBeDisabled()
    act(() => release?.())
    await waitFor(() => expect(refresh).toBeEnabled())
  })

  it("upgrades one package from its menu, offered only when it is upgradable", async () => {
    const { calls } = mockApi(routes({ "POST /api/packages/upgrade": () => noContent() }))
    const { user } = renderApp("/toolchains")
    await user.click((await managedRow("shellcheck")).getByRole("button", { name: "More actions for shellcheck" }))
    expect(await screen.findByRole("menuitem", { name: "Upgrade" })).toHaveAttribute("aria-disabled", "true")
    await user.keyboard("{Escape}")
    await user.click((await managedRow("ripgrep")).getByRole("button", { name: "More actions for ripgrep" }))
    await user.click(await screen.findByRole("menuitem", { name: "Upgrade" }))
    await waitFor(() => expect(calls.find((c) => c.path === "/api/packages/upgrade")?.body).toEqual({ names: ["ripgrep"] }))
    expect((await screen.findAllByText("Queued: upgrade ripgrep")).length).toBeGreaterThan(0)
  })

  it("forgets a package without asking, and leaves it installed", async () => {
    const { calls } = mockApi(routes({ "DELETE /api/packages/ripgrep": () => noContent() }))
    const { user } = renderApp("/toolchains")
    await user.click((await managedRow("ripgrep")).getByRole("button", { name: "More actions for ripgrep" }))
    await user.click(await screen.findByRole("menuitem", { name: "Forget" }))
    await waitFor(() => expect(calls.some((c) => c.method === "DELETE")).toBe(true))
    expect(calls.find((c) => c.method === "DELETE")?.search).toBe("")
    expect((await screen.findAllByText("ripgrep is no longer managed; it stays installed")).length).toBeGreaterThan(0)
  })

  it("uninstalls a package only once confirmed", async () => {
    const { calls } = mockApi(routes({ "DELETE /api/packages/ripgrep": () => noContent() }))
    const { user } = renderApp("/toolchains")
    const open = async () => {
      await user.click((await managedRow("ripgrep")).getByRole("button", { name: "More actions for ripgrep" }))
      await user.click(await screen.findByRole("menuitem", { name: "Uninstall" }))
      return within(await screen.findByRole("alertdialog"))
    }
    let ask = await open()
    expect(ask.getByText("Uninstall ripgrep?")).toBeInTheDocument()
    expect(
      ask.getByText(
        "ghr removes ripgrep with dpkg, which refuses while another installed package depends on it, and drops it from the managed list. It is refused while a job runs or a runner starts, and idle runners stop while it runs.",
      ),
    ).toBeInTheDocument()
    await user.click(ask.getByRole("button", { name: "Cancel" }))
    await waitFor(() => expect(screen.queryByRole("alertdialog")).toBeNull())
    expect(calls.some((c) => c.method === "DELETE")).toBe(false)

    ask = await open()
    await user.click(ask.getByRole("button", { name: "Uninstall" }))
    await waitFor(() => expect(calls.some((c) => c.method === "DELETE" && c.search === "?uninstall=1")).toBe(true))
    expect((await screen.findAllByText("Queued: uninstall ripgrep")).length).toBeGreaterThan(0)
  })

  it("offers no uninstall for a package that is not installed", async () => {
    mockApi(routes())
    const { user } = renderApp("/toolchains")
    await user.click((await managedRow("yq")).getByRole("button", { name: "More actions for yq" }))
    expect(await screen.findByRole("menuitem", { name: "Uninstall" })).toHaveAttribute("aria-disabled", "true")
  })

  it("shows the base packages folded, read-only", async () => {
    mockApi(routes())
    const { user } = renderApp("/toolchains")
    const s = await section()
    const fold = await s.findByRole("button", { name: "Base packages (2)" })
    expect(fold).toHaveAttribute("aria-expanded", "false")
    expect(s.queryByRole("list", { name: "Base packages" })).toBeNull()
    await user.click(fold)
    const list = within(await s.findByRole("list", { name: "Base packages" }))
    expect(list.getAllByRole("listitem").map((li) => li.firstChild?.textContent)).toEqual(["curl", "jq"])
    expect(list.queryByRole("button")).toBeNull()
  })

  it("offers only Forget on a base package that is also managed", async () => {
    const base = [...(fixtures.packages.base ?? []), { name: "git", version: "1:2.47.3-0", installed: true, bytes: 1, managed: true }]
    const { calls } = mockApi(routes({ "GET /api/packages": { ...fixtures.packages, base }, "DELETE /api/packages/git": () => noContent() }))
    const { user } = renderApp("/toolchains")
    const s = await section()
    await user.click(await s.findByRole("button", { name: "Base packages (3)" }))
    const list = within(await s.findByRole("list", { name: "Base packages" }))
    expect(list.getAllByRole("button").map((b) => b.getAttribute("aria-label"))).toEqual(["Forget git"])
    await user.click(list.getByRole("button", { name: "Forget git" }))
    await waitFor(() => expect(calls.some((c) => c.method === "DELETE" && c.path === "/api/packages/git" && c.search === "")).toBe(true))
    expect((await screen.findAllByText("git is no longer managed; it stays installed")).length).toBeGreaterThan(0)
  })

  it("adopts a package installed by hand, filtered by name", async () => {
    const detected = [...(fixtures.packages.detected ?? []), { name: "zstd", version: "1.5.7", installed: true, bytes: 1 }]
    const { calls } = mockApi(
      routes({ "GET /api/packages": { ...fixtures.packages, detected }, "POST /api/packages": { adopt: ["vim"], install: [] } }),
    )
    const { user } = renderApp("/toolchains")
    const s = await section()
    await user.click(await s.findByRole("button", { name: "Installed, not managed (2)" }))
    await user.type(s.getByRole("textbox", { name: "Filter installed packages" }), "vi")
    const list = within(s.getByRole("list", { name: "Installed, not managed" }))
    expect(list.getAllByRole("listitem")).toHaveLength(1)
    await user.click(list.getByRole("button", { name: "Add vim" }))
    await waitFor(() => expect(calls.find((c) => c.method === "POST" && c.path === "/api/packages")?.body).toEqual({ names: ["vim"] }))
    expect((await screen.findAllByText("Queued: adopt vim")).length).toBeGreaterThan(0)
  })

  it("refetches the list when the packages or config topic changes", async () => {
    const { calls } = mockApi(routes())
    const { queryClient } = renderApp("/toolchains")
    await managedRow("ripgrep")
    await waitFor(() => expect(packageCalls(calls)).toBe(1))
    act(() => invalidateTopics(queryClient, ["packages"]))
    await waitFor(() => expect(packageCalls(calls)).toBe(2))
    act(() => invalidateTopics(queryClient, ["config"]))
    await waitFor(() => expect(packageCalls(calls)).toBe(3))
  })
})

describe("Add package dialog", () => {
  const searchCalls = (calls: { path: string; search: string }[]) => calls.filter((c) => c.path === "/api/packages/search").map((c) => c.search)

  it("searches once the typing pauses, and installs the picked package", async () => {
    const { calls } = mockApi(
      routes({
        "GET /api/packages/search": fixtures.packageSearch,
        "POST /api/packages": { adopt: [], install: ["ripgrep-all"] },
      }),
    )
    const { user } = renderApp("/toolchains")
    await user.click(await (await section()).findByRole("button", { name: "Add package" }))
    const dialog = within(await screen.findByRole("dialog"))
    expect(dialog.getByRole("button", { name: "Install" })).toBeDisabled()
    await user.type(dialog.getByRole("textbox", { name: "Package name" }), "ripgrep")
    const results = within(await dialog.findByRole("list", { name: "Search results" }))
    expect(searchCalls(calls)).toEqual(["?q=ripgrep"])
    const managed = results.getByRole("button", { name: "ripgrep, managed" })
    expect(managed).toBeDisabled()
    await user.click(results.getByRole("button", { name: "ripgrep-all" }))
    expect(dialog.getByText(/installs/)).toHaveTextContent("ghr installs ripgrep-all from the host's apt sources.")
    await user.click(dialog.getByRole("button", { name: "Install" }))
    await waitFor(() => expect(calls.find((c) => c.method === "POST" && c.path === "/api/packages")?.body).toEqual({ names: ["ripgrep-all"] }))
    expect((await screen.findAllByText("Queued: install ripgrep-all")).length).toBeGreaterThan(0)
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull())
  })

  it("says a picked installed package will be adopted", async () => {
    const vim = { name: "vim", version: "2:9.1.1230-2", description: "Vi IMproved", installed: true, bytes: 4194304 }
    const { calls } = mockApi(routes({ "GET /api/packages/search": [vim], "POST /api/packages": { adopt: ["vim"], install: [] } }))
    const { user } = renderApp("/toolchains")
    await user.click(await (await section()).findByRole("button", { name: "Add package" }))
    const dialog = within(await screen.findByRole("dialog"))
    await user.type(dialog.getByRole("textbox", { name: "Package name" }), "vim")
    await user.click(await dialog.findByRole("button", { name: "vim, installed" }))
    expect(dialog.getByText(/is installed already/)).toHaveTextContent("vim is installed already. ghr adopts it: it joins the managed list and nothing is installed.")
    await user.click(dialog.getByRole("button", { name: "Adopt" }))
    await waitFor(() => expect(calls.find((c) => c.method === "POST" && c.path === "/api/packages")?.body).toEqual({ names: ["vim"] }))
    expect((await screen.findAllByText("Queued: adopt vim")).length).toBeGreaterThan(0)
  })

  it("asks nothing for a term the daemon would refuse", async () => {
    const { calls } = mockApi(routes({ "GET /api/packages/search": fixtures.packageSearch }))
    const { user } = renderApp("/toolchains")
    await user.click(await (await section()).findByRole("button", { name: "Add package" }))
    const dialog = within(await screen.findByRole("dialog"))
    await user.type(dialog.getByRole("textbox", { name: "Package name" }), "Rip")
    expect(await dialog.findByText("Type 2 to 64 characters of a-z, 0-9, +, . and -")).toBeInTheDocument()
    await new Promise((r) => setTimeout(r, 400))
    expect(searchCalls(calls)).toEqual([])
  })

  it("keeps the dialog open with the daemon's refusal", async () => {
    mockApi(
      routes({
        "GET /api/packages/search": fixtures.packageSearch,
        "POST /api/packages": () => json({ error: "setup.sh installs this package; ghr does not change it" }, 400),
      }),
    )
    const { user } = renderApp("/toolchains")
    await user.click(await (await section()).findByRole("button", { name: "Add package" }))
    const dialog = within(await screen.findByRole("dialog"))
    await user.type(dialog.getByRole("textbox", { name: "Package name" }), "ripgrep")
    await user.click(await dialog.findByRole("button", { name: "ripgrep-all" }))
    await user.click(dialog.getByRole("button", { name: "Install" }))
    expect(await dialog.findByText("setup.sh installs this package; ghr does not change it")).toBeInTheDocument()
    expect(screen.getByRole("dialog")).toBeInTheDocument()
  })
})
