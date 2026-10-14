import { screen, waitFor, within } from "@testing-library/react"
import { useState } from "react"
import { describe, expect, it } from "vitest"
import type { RepoStorage } from "@/api/types"
import { ConfirmDialog, type Confirm } from "@/components/confirm-dialog"
import { json, mockApi, noContent } from "@/test/api"
import { fixtures } from "@/test/fixtures"
import { renderRoutes } from "@/test/routes"
import { ArtifactRepoDetail } from "./artifact-repo-detail"
import { ArtifactRepoTable } from "./artifact-repo-table"

const darkmem = fixtures.githubStorage.repos[0] as RepoStorage

function Detail({ repo, busy, owner }: { repo: RepoStorage; busy: boolean; owner: string }) {
  const [confirm, setConfirm] = useState<Confirm | null>(null)
  return (
    <>
      <ArtifactRepoDetail repo={repo} owner={owner} busy={busy} offline={false} ask={setConfirm} />
      <ConfirmDialog confirm={confirm} onClose={() => setConfirm(null)} />
    </>
  )
}

async function draw(repo: RepoStorage = darkmem, busy = false, owner = "darkraise") {
  const view = renderRoutes({ "/": () => <Detail repo={repo} busy={busy} owner={owner} />, "/actions/$repo/runs/$id": () => <p>run page</p> }, "/")
  await screen.findByRole("region", { name: `Artifacts of ${repo.name}` })
  return view
}

const artifacts = () => screen.getByRole("region", { name: "Artifacts of darkmem" })
const caches = () => screen.getByRole("region", { name: "Caches of darkmem" })
const row = (name: RegExp) => within(artifacts()).getByRole("row", { name })

describe("ArtifactRepoDetail", () => {
  it.each<[boolean, string]>([
    [true, "/actions/darkmem/runs/102"],
    [false, "https://github.com/darkraise/darkmem/actions/runs/102"],
  ])("links an artifact's run to ghr when covered is %s", async (covered, href) => {
    mockApi({})
    await draw({ ...darkmem, covered })
    expect(within(row(/frontend-dist/)).getByRole("link", { name: "Run 102" })).toHaveAttribute("href", href)
    expect(row(/frontend-dist/)).toHaveTextContent("157.3 MB")
    expect(row(/frontend-dist/)).toHaveTextContent("master")
  })

  it("does not link an uncovered artifact's run while the owner is unknown", async () => {
    mockApi({})
    await draw({ ...darkmem, covered: false }, false, "")
    expect(within(row(/frontend-dist/)).queryByRole("link", { name: "Run 102" })).toBeNull()
    expect(row(/frontend-dist/)).toHaveTextContent("Run 102")
  })

  it("deletes the selected artifacts after asking", async () => {
    const { calls } = mockApi({ "POST /api/github-storage/darkmem/artifacts/delete": () => json({ id: "op-2" }, 202) })
    const { user } = await draw()
    const bulk = within(artifacts()).getByRole("button", { name: "Delete selected" })
    expect(bulk).toBeDisabled()
    await user.click(within(artifacts()).getByRole("checkbox", { name: "Select every artifact of darkmem" }))
    expect(within(artifacts()).getByRole("checkbox", { name: "Select coverage" })).toBeChecked()
    await user.click(within(artifacts()).getByRole("checkbox", { name: "Select coverage" }))
    await user.click(bulk)
    const dialog = await screen.findByRole("alertdialog", { name: "Delete 1 artifact from darkmem?" })
    expect(dialog).toHaveTextContent("Frees 157.3 MB. A run that downloads them later fails.")
    await user.click(within(dialog).getByRole("button", { name: "Delete" }))
    await waitFor(() => expect(calls.find((c) => c.method === "POST")?.body).toEqual({ ids: [9001] }))
    expect((await screen.findAllByText("Delete started")).length).toBeGreaterThan(0)
    expect(within(artifacts()).getByRole("checkbox", { name: "Select frontend-dist" })).not.toBeChecked()
  })

  it("deletes artifacts older than an age, counted from the preview", async () => {
    const { calls } = mockApi({
      "GET /api/github-storage/preview": [{ repo: "darkmem", count: 2, bytes: 209715200 }],
      "POST /api/github-storage/darkmem/artifacts/delete": () => json({ id: "op-2" }, 202),
    })
    const { user } = await draw()
    await user.type(within(artifacts()).getByRole("textbox", { name: "Delete artifacts of darkmem older than" }), "1d")
    await user.click(within(artifacts()).getByRole("button", { name: "Delete older" }))
    const dialog = await screen.findByRole("alertdialog", { name: "Delete darkmem's artifacts older than 1d?" })
    expect(dialog).toHaveTextContent("Up to 2 artifacts (209.7 MB). Skips the artifacts of runs that have not completed. A run that downloads them later fails.")
    expect(calls.find((c) => c.path === "/api/github-storage/preview")?.search).toBe("?cleanup=1d&repo=darkmem")
    await user.click(within(dialog).getByRole("button", { name: "Delete" }))
    await waitFor(() => expect(calls.find((c) => c.method === "POST")?.body).toEqual({ older_than: "1d" }))
  })

  it("previews once when Delete older is double clicked", async () => {
    let answer: (r: Response) => void = () => undefined
    const { calls } = mockApi({ "GET /api/github-storage/preview": () => new Promise<Response>((resolve) => (answer = resolve)) })
    const { user } = await draw()
    await user.type(within(artifacts()).getByRole("textbox", { name: "Delete artifacts of darkmem older than" }), "1d")
    await user.dblClick(within(artifacts()).getByRole("button", { name: "Delete older" }))
    answer(json([{ repo: "darkmem", count: 2, bytes: 209715200 }]))
    expect(await screen.findByRole("alertdialog", { name: "Delete darkmem's artifacts older than 1d?" })).toBeInTheDocument()
    expect(calls.filter((c) => c.path === "/api/github-storage/preview")).toHaveLength(1)
  })

  it("says when nothing is older than the age", async () => {
    const { calls } = mockApi({ "GET /api/github-storage/preview": [] })
    const { user } = await draw()
    await user.type(within(artifacts()).getByRole("textbox", { name: "Delete artifacts of darkmem older than" }), "30d")
    await user.click(within(artifacts()).getByRole("button", { name: "Delete older" }))
    expect((await screen.findAllByText("No artifact of darkmem is older than 30d")).length).toBeGreaterThan(0)
    expect(screen.queryByRole("alertdialog")).toBeNull()
    expect(calls.some((c) => c.method === "POST")).toBe(false)
  })

  it.each<[string, () => HTMLElement, string, string, string]>([
    ["artifact", artifacts, "Delete frontend-dist", "Delete frontend-dist?", "DELETE /api/github-storage/darkmem/artifacts/9001"],
    ["cache", caches, "Delete cache node-cache-Linux-x64-npm-a4cc85d3", "Delete this cache?", "DELETE /api/github-storage/darkmem/caches/8787967584"],
  ])("deletes one %s after asking", async (_kind, region, button, title, route) => {
    const { calls } = mockApi({ [route]: () => noContent() })
    const { user } = await draw()
    await user.click(within(region()).getByRole("button", { name: button }))
    await user.click(within(await screen.findByRole("alertdialog", { name: title })).getByRole("button", { name: "Delete" }))
    await waitFor(() => expect(calls.map((c) => `${c.method} ${c.path}`)).toContain(route))
  })

  it("deletes every cache after asking", async () => {
    const { calls } = mockApi({ "POST /api/github-storage/darkmem/caches/delete": () => json({ id: "op-3" }, 202) })
    const { user } = await draw()
    expect(within(caches()).getByRole("row", { name: /^node-cache-Linux-x64-npm-a4cc85d3/ })).toHaveTextContent("refs/heads/master")
    await user.click(within(caches()).getByRole("button", { name: "Delete all caches" }))
    const dialog = await screen.findByRole("alertdialog", { name: "Delete 1 cache of darkmem?" })
    expect(dialog).toHaveTextContent("Frees 71.4 MB. Jobs rebuild what they need.")
    await user.click(within(dialog).getByRole("button", { name: "Delete" }))
    await waitFor(() => expect(calls.find((c) => c.method === "POST")?.body).toEqual({ all: true }))
  })

  it("holds the bulk deletes while an operation runs", async () => {
    mockApi({})
    const { user } = await draw(darkmem, true)
    await user.click(within(artifacts()).getByRole("checkbox", { name: "Select coverage" }))
    await user.type(within(artifacts()).getByRole("textbox", { name: "Delete artifacts of darkmem older than" }), "1d")
    for (const name of ["Delete selected", "Delete older"]) expect(within(artifacts()).getByRole("button", { name })).toBeDisabled()
    expect(within(caches()).getByRole("button", { name: "Delete all caches" })).toBeDisabled()
    expect(within(artifacts()).getByRole("button", { name: "Delete frontend-dist" })).toBeEnabled()
  })

  it("says when a repository has nothing stored", async () => {
    mockApi({})
    await draw({ ...darkmem, name: "empty", artifacts: [], caches: [], artifact_count: 0, cache_count: 0 })
    expect(within(screen.getByRole("region", { name: "Artifacts of empty" })).getByText("No artifacts.")).toBeInTheDocument()
    expect(within(screen.getByRole("region", { name: "Caches of empty" })).getByText("No caches.")).toBeInTheDocument()
  })
})

describe("ArtifactRepoTable rows", () => {
  it("expand to show the repository's artifacts and caches", async () => {
    mockApi({})
    const { user } = renderRoutes(
      { "/": () => <ArtifactRepoTable storage={fixtures.githubStorage} config={fixtures.config} offline={false} /> },
      "/",
    )
    const toggle = await screen.findByRole("button", { name: "darkcloud" })
    expect(toggle).toHaveAttribute("aria-expanded", "false")
    expect(screen.queryByRole("region", { name: "Artifacts of darkcloud" })).toBeNull()
    await user.click(toggle)
    expect(toggle).toHaveAttribute("aria-expanded", "true")
    expect(within(screen.getByRole("region", { name: "Artifacts of darkcloud" })).getByRole("link", { name: "Run 501" })).toHaveAttribute(
      "href",
      "/actions/darkcloud/runs/501",
    )
    await user.click(toggle)
    expect(screen.queryByRole("region", { name: "Artifacts of darkcloud" })).toBeNull()
  })
})
