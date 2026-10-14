import { screen, waitFor, within } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import type { GitHubStorage, RepoStorage } from "@/api/types"
import { json, mockApi, noContent } from "@/test/api"
import { fixtures } from "@/test/fixtures"
import { renderRoutes } from "@/test/routes"
import { ArtifactRepoTable } from "./artifact-repo-table"

async function draw(storage: GitHubStorage = fixtures.githubStorage) {
  const view = renderRoutes({ "/": () => <ArtifactRepoTable storage={storage} config={fixtures.config} offline={false} /> }, "/")
  await screen.findByRole("region", { name: "Repositories" })
  return view
}

const row = (name: string) => screen.getByRole("row", { name: new RegExp(`^${name}\\b`) })

function repo(name: string, over: Partial<RepoStorage> = {}): RepoStorage {
  return {
    name,
    visibility: "private",
    covered: true,
    artifact_count: 0,
    artifact_bytes: 0,
    cache_count: 0,
    cache_bytes: 0,
    retention_days: 7,
    retention_max_days: 400,
    cleanup: "off",
    cleanup_source: "none",
    artifacts: [],
    caches: [],
    ...over,
  }
}

// The body rows' repository names; the first row is the header, the last the totals.
const listed = () =>
  screen
    .getAllByRole("row")
    .slice(1, -1)
    .map((r) => within(r).getByRole("rowheader").textContent?.trim().split(" ")[0])

describe("ArtifactRepoTable", () => {
  it("lists each repository's artifacts, caches, retention and cleanup", async () => {
    mockApi({})
    await draw()
    const rows = screen.getAllByRole("row").slice(1, -1)
    expect(rows.map((r) => within(r).getByRole("rowheader").textContent)).toEqual([
      expect.stringMatching(/^darkmem/),
      expect.stringMatching(/^darkcloud/),
      expect.stringMatching(/^sandbox/),
    ])
    const darkmem = row("darkmem")
    for (const text of ["209.7 MB", "2 artifacts", "71.4 MB", "1 cache", "90 days", "6h (override)"]) {
      expect(within(darkmem).getByText(text)).toBeInTheDocument()
    }
    expect(within(row("darkcloud")).getByText("2d (global)")).toBeInTheDocument()
    const sandbox = row("sandbox")
    expect(within(sandbox).getByText("public")).toBeInTheDocument()
    expect(within(sandbox).getByText("Unknown")).toBeInTheDocument()
    expect(within(sandbox).getByText("token lacks actions=read on darkraise/sandbox: grant it in the PAT settings")).toBeInTheDocument()
  })

  it.each<[string, string | null]>([
    ["warn", "1 repository keeps GitHub's 90-day default; lowering retention saves storage."],
    ["", null],
  ])("at level %j, the retention line reads %j", async (level, line) => {
    mockApi({})
    const quota = fixtures.githubStorage.quota && { ...fixtures.githubStorage.quota, level }
    await draw({ ...fixtures.githubStorage, quota })
    expect(screen.queryByText(/90-day default/)?.textContent ?? null).toBe(line)
  })

  it("hides empty repositories behind a switch, largest artifacts first, then caches, then name", async () => {
    mockApi({})
    const { user } = await draw({
      ...fixtures.githubStorage,
      repos: [
        repo("Beta"),
        repo("alpha"),
        repo("beta", { cache_count: 1, cache_bytes: 500 }),
        repo("gamma", { artifact_count: 1, artifact_bytes: 10 }),
        repo("delta", { cache_count: 2, cache_bytes: 900 }),
        repo("epsilon", { artifact_count: 1, artifact_bytes: 10, cache_count: 1, cache_bytes: 5 }),
      ],
    })
    expect(listed()).toEqual(["epsilon", "gamma", "delta", "beta"])
    await user.click(screen.getByRole("switch", { name: "Show 2 empty repositories" }))
    expect(listed()).toEqual(["epsilon", "gamma", "delta", "beta", "alpha", "Beta"])
  })

  it("filters by name ignoring case, showing a matching empty repository", async () => {
    mockApi({})
    const { user } = await draw({
      ...fixtures.githubStorage,
      repos: [repo("Alpha"), repo("alpine", { artifact_count: 1, artifact_bytes: 10 }), repo("beta", { artifact_count: 1, artifact_bytes: 20 })],
    })
    const filter = screen.getByRole("textbox", { name: "Filter repositories" })
    await user.type(filter, "ALP")
    expect(listed()).toEqual(["alpine", "Alpha"])
    await user.clear(filter)
    await user.type(filter, "nope")
    expect(screen.getByText("No repository matches.")).toBeInTheDocument()
  })

  it("totals every repository whatever the filter shows", async () => {
    mockApi({})
    const { user } = await draw()
    await user.type(screen.getByRole("textbox", { name: "Filter repositories" }), "darkcloud")
    const total = screen.getByRole("row", { name: /^All 3 repositories/ })
    for (const text of ["230.7 MB", "3 artifacts", "71.4 MB", "1 cache"]) {
      expect(within(total).getByText(text)).toBeInTheDocument()
    }
  })

  it("sets retention on every repository after a warning", async () => {
    const { calls } = mockApi({ "POST /api/github-storage/retention": () => json({ id: "op-2" }, 202) })
    const { user } = await draw()
    await user.type(screen.getByRole("spinbutton", { name: "Retention for all" }), "7")
    await user.click(screen.getByRole("button", { name: "Set retention" }))
    const dialog = await screen.findByRole("alertdialog", { name: "Set retention to 7 days on 3 repositories?" })
    expect(calls.some((c) => c.method === "POST")).toBe(false)
    expect(dialog).toHaveTextContent(
      "GitHub's retention also limits how long it keeps runs and logs, which ghr's Actions and log pages read. A repository whose maximum is lower is skipped.",
    )
    await user.click(within(dialog).getByRole("button", { name: "Set retention" }))
    await waitFor(() => expect(calls.find((c) => c.method === "POST")?.body).toEqual({ days: 7 }))
    expect((await screen.findAllByText("Retention change started")).length).toBeGreaterThan(0)
  })

  it("ties the days field to its error", async () => {
    mockApi({})
    const { user } = await draw()
    const input = screen.getByRole("spinbutton", { name: "Retention for all" })
    expect(input).not.toHaveAttribute("aria-invalid", "true")
    await user.type(input, "500")
    expect(input).toHaveAttribute("aria-invalid", "true")
    expect(input).toHaveAccessibleDescription("Enter a whole number of days from 1 to 400")
  })

  it("takes a fraction typed into the days field as whole days", async () => {
    mockApi({})
    const { user } = await draw()
    const input = screen.getByRole("spinbutton", { name: "Retention for all" })
    await user.type(input, "30.5")
    expect(input).toHaveValue("30.5")
    expect(input).not.toHaveAttribute("aria-invalid", "true")
    expect(screen.getByRole("button", { name: "Set retention" })).toBeEnabled()
  })

  it("holds Set retention while an operation runs", async () => {
    mockApi({})
    const operation = fixtures.githubStorage.operation && { ...fixtures.githubStorage.operation, finished_at: undefined }
    const { user } = await draw({ ...fixtures.githubStorage, operation })
    await user.type(screen.getByRole("spinbutton", { name: "Retention for all" }), "7")
    expect(screen.getByRole("button", { name: "Set retention" })).toBeDisabled()
  })

  it("sets one repository's retention after a warning", async () => {
    const { calls } = mockApi({ "PUT /api/github-storage/darkcloud/retention": () => noContent() })
    const { user } = await draw()
    await user.click(screen.getByRole("button", { name: "Edit retention of darkcloud" }))
    const input = screen.getByRole("spinbutton", { name: "Retention of darkcloud" })
    await user.clear(input)
    await user.type(input, "120")
    expect(screen.getByText("Enter a whole number of days from 1 to 90")).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Save retention of darkcloud" })).toBeDisabled()
    await user.clear(input)
    await user.type(input, "3")
    await user.click(screen.getByRole("button", { name: "Save retention of darkcloud" }))
    const dialog = await screen.findByRole("alertdialog", { name: "Set retention of darkcloud to 3 days?" })
    expect(dialog).toHaveTextContent("GitHub's retention also limits how long it keeps runs and logs, which ghr's Actions and log pages read.")
    await user.click(within(dialog).getByRole("button", { name: "Set retention" }))
    await waitFor(() => expect(calls.find((c) => c.method === "PUT")?.body).toEqual({ days: 3 }))
    expect((await screen.findAllByText("Retention of darkcloud set to 3 days")).length).toBeGreaterThan(0)
  })

  it.each<[string, string, string, string | null, string | null]>([
    ["darkcloud", "age", "12h", "?cleanup=12h&repo=darkcloud", "12h"],
    ["darkmem", "inherit", "", "?cleanup=2d&repo=darkmem", null],
  ])("previews %s's cleanup as %s before saving it", async (name, choice, age, search, sent) => {
    const { calls } = mockApi({
      "GET /api/github-storage/preview": [{ repo: name, count: 1, bytes: 20971520 }],
      "PATCH /api/config": () => noContent(),
    })
    const { user } = await draw()
    await user.click(screen.getByRole("button", { name: `Edit cleanup of ${name}` }))
    await user.selectOptions(screen.getByRole("combobox", { name: `Cleanup of ${name}` }), choice)
    if (age) await user.type(screen.getByRole("textbox", { name: `Cleanup age of ${name}` }), age)
    await user.click(screen.getByRole("button", { name: `Save cleanup of ${name}` }))
    const effective = age || "2d"
    const dialog = await screen.findByRole("alertdialog", { name: `Clean ${name}'s artifacts after ${effective}?` })
    expect(dialog).toHaveTextContent(`Up to 1 artifact (21.0 MB) older than ${effective} would go at the next check.`)
    expect(calls.find((c) => c.path === "/api/github-storage/preview")?.search).toBe(search)
    await user.click(within(dialog).getByRole("button", { name: "Save" }))
    await waitFor(() => expect(calls.find((c) => c.method === "PATCH")?.body).toEqual({ github_storage: { repos: { [name]: sent } } }))
    expect((await screen.findAllByText(`Cleanup of ${name} saved`)).length).toBeGreaterThan(0)
  })

  it("turns one repository's cleanup off without a preview", async () => {
    const { calls } = mockApi({ "PATCH /api/config": () => noContent() })
    const { user } = await draw()
    await user.click(screen.getByRole("button", { name: "Edit cleanup of darkcloud" }))
    await user.selectOptions(screen.getByRole("combobox", { name: "Cleanup of darkcloud" }), "off")
    await user.click(screen.getByRole("button", { name: "Save cleanup of darkcloud" }))
    await waitFor(() => expect(calls.find((c) => c.method === "PATCH")?.body).toEqual({ github_storage: { repos: { darkcloud: "off" } } }))
    expect(calls.some((c) => c.path === "/api/github-storage/preview")).toBe(false)
  })

  it("keeps the cleanup edit disabled until the config has loaded", async () => {
    mockApi({})
    renderRoutes({ "/": () => <ArtifactRepoTable storage={fixtures.githubStorage} config={undefined} offline={false} /> }, "/")
    await screen.findByRole("region", { name: "Repositories" })
    expect(screen.getByRole("button", { name: "Edit cleanup of darkcloud" })).toBeDisabled()
  })

  it("shows a refused preview and saves nothing", async () => {
    const { calls } = mockApi({ "GET /api/github-storage/preview": json({ error: "invalid duration \"1x\"" }, 400) })
    const { user } = await draw()
    await user.click(screen.getByRole("button", { name: "Edit cleanup of darkcloud" }))
    await user.selectOptions(screen.getByRole("combobox", { name: "Cleanup of darkcloud" }), "age")
    await user.type(screen.getByRole("textbox", { name: "Cleanup age of darkcloud" }), "1x")
    await user.click(screen.getByRole("button", { name: "Save cleanup of darkcloud" }))
    expect((await screen.findAllByText("invalid duration \"1x\"")).length).toBeGreaterThan(0)
    expect(calls.some((c) => c.method === "PATCH")).toBe(false)
  })
})
