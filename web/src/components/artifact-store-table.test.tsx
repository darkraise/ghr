import { screen, waitFor, within } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import { keys } from "@/api/hooks"
import type { ArtifactStore, ArtifactStoreRepo } from "@/api/types"
import { json, mockApi } from "@/test/api"
import { fixtures } from "@/test/fixtures"
import { renderRoutes } from "@/test/routes"
import { ArtifactStoreTable } from "./artifact-store-table"

// Each row's state set here, so the tests read the same whatever the fixture says.
const STATES: Record<string, Partial<ArtifactStoreRepo>> = {
  darkmem: { state: "on", enabled: true, private: true },
  darkcloud: { state: "on", enabled: true, private: true },
  docs: { state: "off", enabled: false, private: true },
  "old-docs": { state: "public", enabled: false, private: false },
}
const base: ArtifactStore = { ...fixtures.artifactStore, repos: fixtures.artifactStore.repos.map((r) => ({ ...r, ...STATES[r.name] })) }

async function draw(store: ArtifactStore = base) {
  const view = renderRoutes(
    { "/": () => <ArtifactStoreTable store={store} config={fixtures.config} offline={false} />, "/actions/$repo/runs/$id": () => <p>run page</p> },
    "/",
  )
  view.queryClient.setQueryData(keys.status, fixtures.status)
  await screen.findByRole("region", { name: "Repositories" })
  return view
}

const repoOf = (name: string) => base.repos.find((r) => r.name === name) as ArtifactStoreRepo
const withRepo = (name: string, over: Partial<ArtifactStoreRepo>): ArtifactStore => ({
  ...base,
  repos: base.repos.map((r) => (r.name === name ? { ...r, ...over } : r)),
})
const row = (name: string) => screen.getByRole("row", { name: new RegExp(`^${name}\\b`) })
const toggle = (name: string) => screen.getByRole("switch", { name: `Use RustFS for ${name}` })
const noToggle = (name: string) => expect(screen.queryByRole("switch", { name: `Use RustFS for ${name}` })).toBeNull()
const bodies = (calls: { method: string; path: string; body: unknown }[], method: string, path: string) =>
  calls.filter((c) => c.method === method && c.path === path).map((c) => c.body)
// The body rows' repository names; the first row is the header, the last the totals.
const listed = () =>
  screen
    .getAllByRole("row")
    .slice(1, -1)
    .map((r) => within(r).getByRole("rowheader").textContent?.trim().split(" ")[0])
const NOT_SERVED = "Not served by ghr · only repositories ghr runs jobs for can use self-hosted storage"

describe("ArtifactStoreTable", () => {
  it("lists each repository's switch, state, size and cleanup, largest first", async () => {
    mockApi({})
    await draw()
    expect(listed()).toEqual(["darkcloud", "darkmem", "docs", "old-docs"])
    const darkmem = row("darkmem")
    expect(toggle("darkmem")).toBeChecked()
    for (const text of ["On", "13.8 MB", "4 artifacts", "3d (override)"]) {
      expect(within(darkmem).getByText(text)).toBeInTheDocument()
    }
    expect(within(darkmem).getByText("On · uploads from ghr's runners go to RustFS")).toHaveClass("text-success")
    expect(within(row("darkcloud")).getByText("7d (global)")).toBeInTheDocument()
    expect(toggle("docs")).not.toBeChecked()
    expect(within(row("docs")).getByText("Off · uploads go to GitHub")).toHaveClass("text-muted-foreground")
    const oldDocs = row("old-docs")
    noToggle("old-docs")
    expect(within(oldDocs).getByText("public")).toBeInTheDocument()
    expect(within(oldDocs).getByText("Public repositories stay on GitHub")).toBeInTheDocument()
    expect(within(oldDocs).queryByRole("button", { name: "Edit cleanup of old-docs" })).toBeNull()
    const totals = screen.getAllByRole("row").at(-1) as HTMLElement
    for (const text of ["All 4 repositories", "423.8 MB", "5 artifacts"]) expect(within(totals).getByText(text)).toBeInTheDocument()
    expect(
      screen.getByText(
        "Only private repositories ghr serves can be turned on. Their jobs on ghr's runners then upload to RustFS; jobs on other runners, GitHub-hosted ones included, keep using GitHub and cannot download what ghr's runners stored.",
      ),
    ).toBeInTheDocument()
    expect(screen.queryByText(/GHR_ARTIFACT_STORE|GHR_ARTIFACT_KEY|secret/)).toBeNull()
  })

  it("offers no switch on a repository ghr does not serve", async () => {
    mockApi({})
    await draw(withRepo("docs", { state: "not_served", enabled: false }))
    noToggle("docs")
    expect(within(row("docs")).getByText(NOT_SERVED)).toHaveClass("text-muted-foreground")
  })

  it.each<[string, ArtifactStore, string]>([
    ["a repository ghr no longer serves", withRepo("docs", { state: "not_served", enabled: true }), "docs"],
    ["a public repository", withRepo("old-docs", { enabled: true }), "old-docs"],
  ])("offers only a switch-off on %s that is still on", async (_name, store, name) => {
    const { calls } = mockApi({ [`PUT /api/artifact-store/repos/${name}`]: { ...repoOf(name), enabled: false } })
    const { user } = await draw(store)
    expect(toggle(name)).toBeChecked()
    await user.click(toggle(name))
    await waitFor(() => expect(bodies(calls, "PUT", `/api/artifact-store/repos/${name}`)).toEqual([{ enabled: false }]))
    expect((await screen.findAllByText(`Turned off for ${name}: its artifacts stay until they expire`)).length).toBeGreaterThan(0)
  })

  it("warns on a repository ghr no longer serves whose entry is still on", async () => {
    mockApi({})
    await draw(withRepo("docs", { state: "not_served", enabled: true }))
    expect(within(row("docs")).getByText(NOT_SERVED)).toHaveClass("text-warning")
  })

  it.each<[string, string, boolean, Partial<ArtifactStoreRepo>, string]>([
    ["turns a repository on", "docs", true, { enabled: true, state: "on" }, "Turned on for docs: its next runners get the store settings"],
    ["turns a repository off and keeps its artifacts", "darkmem", false, { enabled: false, state: "off" }, "Turned off for darkmem: its artifacts stay until they expire"],
  ])("%s", async (_name, name, enabled, answer, message) => {
    const { calls } = mockApi({ [`PUT /api/artifact-store/repos/${name}`]: { ...repoOf(name), ...answer } })
    const { user } = await draw()
    await user.click(toggle(name))
    await waitFor(() => expect(bodies(calls, "PUT", `/api/artifact-store/repos/${name}`)).toEqual([{ enabled }]))
    expect((await screen.findAllByText(message)).length).toBeGreaterThan(0)
  })

  it("shows the daemon's refusal of a switch", async () => {
    const refusal = "docs is not served by ghr; self-hosted storage serves only repositories ghr runs jobs for"
    mockApi({ "PUT /api/artifact-store/repos/docs": () => json({ error: refusal }, 409) })
    const { user } = await draw()
    await user.click(toggle("docs"))
    expect((await screen.findAllByText(refusal)).length).toBeGreaterThan(0)
  })

  it("turns every served private repository on after naming them", async () => {
    const answer = [repoOf("darkmem"), repoOf("darkcloud"), { ...repoOf("docs"), enabled: true, state: "on" }]
    const { calls } = mockApi({ "POST /api/artifact-store/repos/enable-private": answer })
    const { user } = await draw()
    await user.click(screen.getByRole("button", { name: "Turn on for all private repositories" }))
    const dialog = await screen.findByRole("alertdialog", { name: "Turn on RustFS for 1 repository?" })
    expect(dialog).toHaveTextContent("Jobs on ghr's runners for docs then upload to RustFS. Jobs on other runners keep using GitHub.")
    expect(bodies(calls, "POST", "/api/artifact-store/repos/enable-private")).toHaveLength(0)
    await user.click(within(dialog).getByRole("button", { name: "Turn on" }))
    expect((await screen.findAllByText("Turned on for every private repository ghr serves: their next runners get the store settings")).length).toBeGreaterThan(0)
    expect(bodies(calls, "POST", "/api/artifact-store/repos/enable-private")).toHaveLength(1)
  })

  it("says when every served private repository is already on, leaving one ghr does not serve alone", async () => {
    const { calls } = mockApi({})
    const { user } = await draw(withRepo("docs", { state: "not_served", enabled: false }))
    await user.click(screen.getByRole("button", { name: "Turn on for all private repositories" }))
    expect((await screen.findAllByText("Every private repository ghr serves is already on")).length).toBeGreaterThan(0)
    expect(screen.queryByRole("alertdialog")).toBeNull()
    expect(calls.filter((c) => c.method === "POST")).toHaveLength(0)
  })

  it("asks before a cleanup age that would remove listed artifacts, then saves it", async () => {
    const { calls } = mockApi({ "PUT /api/artifact-store/repos/darkmem": { ...repoOf("darkmem"), cleanup: "1d" } })
    const { user } = await draw()
    await user.click(within(row("darkmem")).getByRole("button", { name: "Edit cleanup of darkmem" }))
    const age = screen.getByRole("textbox", { name: "Cleanup age of darkmem" })
    expect(age).toHaveValue("3d")
    await user.clear(age)
    await user.type(age, "1d")
    await user.click(screen.getByRole("button", { name: "Save cleanup of darkmem" }))
    const dialog = await screen.findByRole("alertdialog", { name: "Clean darkmem's artifacts after 1d?" })
    expect(dialog).toHaveTextContent("1 artifact (2.4 MB) older than 1d would go at the next check")
    expect(bodies(calls, "PUT", "/api/artifact-store/repos/darkmem")).toHaveLength(0)
    await user.click(within(dialog).getByRole("button", { name: "Save" }))
    await waitFor(() => expect(bodies(calls, "PUT", "/api/artifact-store/repos/darkmem")).toEqual([{ cleanup: "1d" }]))
    expect((await screen.findAllByText("Cleanup of darkmem saved")).length).toBeGreaterThan(0)
  })

  it.each<[string, string, unknown]>([
    ["goes back to the global age", "inherit", { cleanup: "" }],
    ["turns cleanup off", "off", { cleanup: "off" }],
  ])("%s without asking when nothing listed would go", async (_name, choice, body) => {
    const { calls } = mockApi({ "PUT /api/artifact-store/repos/darkmem": repoOf("darkmem") })
    const { user } = await draw()
    await user.click(within(row("darkmem")).getByRole("button", { name: "Edit cleanup of darkmem" }))
    const select = screen.getByRole("combobox", { name: "Cleanup of darkmem" })
    expect(within(select).getByRole("option", { name: "Global (7d)" })).toBeInTheDocument()
    await user.selectOptions(select, choice)
    await user.click(screen.getByRole("button", { name: "Save cleanup of darkmem" }))
    await waitFor(() => expect(bodies(calls, "PUT", "/api/artifact-store/repos/darkmem")).toEqual([body]))
    expect(screen.queryByRole("alertdialog")).toBeNull()
  })

  it("expands a repository to its artifacts", async () => {
    mockApi({})
    const { user } = await draw()
    const button = screen.getByRole("button", { name: "darkmem" })
    expect(button).toHaveAttribute("aria-expanded", "false")
    await user.click(button)
    expect(button).toHaveAttribute("aria-expanded", "true")
    const e2e = within(screen.getByRole("region", { name: "Artifacts of darkmem" })).getByRole("row", { name: /^e2e-report/ })
    expect(within(e2e).getByRole("link", { name: "Run 102" })).toHaveAttribute("href", "/actions/darkmem/runs/102")
  })

  it("filters the repositories by name", async () => {
    mockApi({})
    const { user } = await draw()
    await user.type(screen.getByRole("textbox", { name: "Filter repositories" }), "cloud")
    expect(listed()).toEqual(["darkcloud"])
    await user.clear(screen.getByRole("textbox", { name: "Filter repositories" }))
    await user.type(screen.getByRole("textbox", { name: "Filter repositories" }), "zzz")
    expect(screen.getByText("No repository matches.")).toBeInTheDocument()
  })
})
