import { screen, waitFor, within } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import { keys } from "@/api/hooks"
import type { ArtifactStore, ArtifactStoreRepo } from "@/api/types"
import { json, mockApi } from "@/test/api"
import { fixtures } from "@/test/fixtures"
import { renderRoutes } from "@/test/routes"
import { ArtifactStoreTable } from "./artifact-store-table"

async function draw(store: ArtifactStore = fixtures.artifactStore) {
  const view = renderRoutes(
    { "/": () => <ArtifactStoreTable store={store} config={fixtures.config} offline={false} />, "/actions/$repo/runs/$id": () => <p>run page</p> },
    "/",
  )
  view.queryClient.setQueryData(keys.status, fixtures.status)
  await screen.findByRole("region", { name: "Repositories" })
  return view
}

const repoOf = (name: string) => fixtures.artifactStore.repos.find((r) => r.name === name) as ArtifactStoreRepo
const withRepo = (name: string, over: Partial<ArtifactStoreRepo>): ArtifactStore => ({
  ...fixtures.artifactStore,
  repos: fixtures.artifactStore.repos.map((r) => (r.name === name ? { ...r, ...over } : r)),
})
const docsOff = withRepo("docs", { state: "off", state_error: undefined })
const row = (name: string) => screen.getByRole("row", { name: new RegExp(`^${name}\\b`) })
const toggle = (name: string) => screen.getByRole("switch", { name: `Use RustFS for ${name}` })
const bodies = (calls: { method: string; path: string; body: unknown }[], method: string, path: string) =>
  calls.filter((c) => c.method === method && c.path === path).map((c) => c.body)
// The body rows' repository names; the first row is the header, the last the totals.
const listed = () =>
  screen
    .getAllByRole("row")
    .slice(1, -1)
    .map((r) => within(r).getByRole("rowheader").textContent?.trim().split(" ")[0])

describe("ArtifactStoreTable", () => {
  it("lists each repository's switch, state, size and cleanup, largest first", async () => {
    mockApi({})
    await draw()
    expect(listed()).toEqual(["darkcloud", "darkmem", "docs", "old-docs"])
    const darkmem = row("darkmem")
    expect(toggle("darkmem")).toBeChecked()
    for (const text of ["On", "Variable and secret set · uploads go to RustFS", "13.8 MB", "4 artifacts", "3d (override)"]) {
      expect(within(darkmem).getByText(text)).toBeInTheDocument()
    }
    const failed = within(row("darkcloud")).getByText("token lacks secrets=write on darkraise/darkcloud: grant it in the PAT settings")
    expect(failed).toHaveClass("text-destructive")
    expect(within(row("darkcloud")).getByText("7d (global)")).toBeInTheDocument()
    expect(toggle("docs")).not.toBeChecked()
    expect(within(row("docs")).getByText(/which is not this ghr's storage; ghr leaves it alone$/)).toHaveClass("text-warning")
    const oldDocs = row("old-docs")
    expect(toggle("old-docs")).toBeDisabled()
    expect(within(oldDocs).getByText("public")).toBeInTheDocument()
    expect(within(oldDocs).getByText("Public repositories stay on GitHub")).toBeInTheDocument()
    expect(within(oldDocs).queryByRole("button", { name: "Edit cleanup of old-docs" })).toBeNull()
    const totals = screen.getAllByRole("row").at(-1) as HTMLElement
    for (const text of ["All 4 repositories", "423.8 MB", "5 artifacts"]) expect(within(totals).getByText(text)).toBeInTheDocument()
    expect(screen.getByText("Turning a repository on sets its GHR_ARTIFACT_STORE variable and GHR_ARTIFACT_KEY secret; its workflows then upload to RustFS.")).toBeInTheDocument()
  })

  it.each<[string, ArtifactStore, string, boolean, Partial<ArtifactStoreRepo>, string, "success" | "error"]>([
    ["turns a repository on and says what ghr set", docsOff, "docs", true, { enabled: true, state: "set" }, "Turned on for docs: variable and secret set", "success"],
    [
      "turns a repository on whose writes wait for the next check",
      docsOff,
      "docs",
      true,
      { enabled: true, state: "missing" },
      "Turned on for docs; the next check writes its variable and secret",
      "success",
    ],
    [
      "turns a repository off and keeps its artifacts",
      fixtures.artifactStore,
      "darkmem",
      false,
      { enabled: false, state: "off" },
      "Turned off for darkmem: variable and secret removed; its artifacts stay until they expire",
      "success",
    ],
    [
      "shows why the variable and secret could not be set",
      docsOff,
      "docs",
      true,
      { enabled: true, state: "error", state_error: "token lacks secrets=write on darkraise/docs: grant it in the PAT settings" },
      "docs: token lacks secrets=write on darkraise/docs: grant it in the PAT settings",
      "error",
    ],
  ])("%s", async (_name, store, name, enabled, answer, message) => {
    const { calls } = mockApi({ [`PUT /api/artifact-store/repos/${name}`]: { ...repoOf(name), state_error: undefined, ...answer } })
    const { user } = await draw(store)
    await user.click(toggle(name))
    await waitFor(() => expect(bodies(calls, "PUT", `/api/artifact-store/repos/${name}`)).toEqual([{ enabled }]))
    expect((await screen.findAllByText(message)).length).toBeGreaterThan(0)
  })

  it("shows the daemon's refusal of a switch", async () => {
    mockApi({ "PUT /api/artifact-store/repos/docs": () => json({ error: "set the job key first" }, 409) })
    const { user } = await draw(docsOff)
    await user.click(toggle("docs"))
    expect((await screen.findAllByText("set the job key first")).length).toBeGreaterThan(0)
  })

  it("turns every private repository on after naming them", async () => {
    const answer = [repoOf("darkmem"), { ...repoOf("darkcloud"), state: "set", state_error: undefined }, { ...repoOf("docs"), enabled: true, state: "set", state_error: undefined }]
    const { calls } = mockApi({ "POST /api/artifact-store/repos/enable-private": answer })
    const { user } = await draw(docsOff)
    await user.click(screen.getByRole("button", { name: "Turn on for all private repositories" }))
    const dialog = await screen.findByRole("alertdialog", { name: "Turn on RustFS for 1 repository?" })
    expect(dialog).toHaveTextContent("ghr sets the GHR_ARTIFACT_STORE variable and GHR_ARTIFACT_KEY secret of docs. Its workflows then upload to RustFS.")
    expect(bodies(calls, "POST", "/api/artifact-store/repos/enable-private")).toHaveLength(0)
    await user.click(within(dialog).getByRole("button", { name: "Turn on" }))
    expect((await screen.findAllByText("Turned on for every private repository")).length).toBeGreaterThan(0)
    expect(bodies(calls, "POST", "/api/artifact-store/repos/enable-private")).toHaveLength(1)
  })

  it("names the repositories the bulk turn-on could not set", async () => {
    const answer = [repoOf("darkmem"), repoOf("darkcloud"), { ...repoOf("docs"), enabled: true, state: "set", state_error: undefined }]
    mockApi({ "POST /api/artifact-store/repos/enable-private": answer })
    const { user } = await draw(docsOff)
    await user.click(screen.getByRole("button", { name: "Turn on for all private repositories" }))
    await user.click(within(await screen.findByRole("alertdialog")).getByRole("button", { name: "Turn on" }))
    expect((await screen.findAllByText("Could not turn on darkcloud; its row says why")).length).toBeGreaterThan(0)
  })

  it("says when every private repository is already on, leaving another ghr's repository alone", async () => {
    const { calls } = mockApi({})
    const { user } = await draw()
    await user.click(screen.getByRole("button", { name: "Turn on for all private repositories" }))
    expect((await screen.findAllByText("Every private repository ghr can change is already on")).length).toBeGreaterThan(0)
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
