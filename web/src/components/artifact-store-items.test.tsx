import { screen, waitFor, within } from "@testing-library/react"
import { useState } from "react"
import { afterEach, describe, expect, it, vi } from "vitest"
import { keys } from "@/api/hooks"
import type { ArtifactStoreRepo } from "@/api/types"
import { ConfirmDialog, type Confirm } from "@/components/confirm-dialog"
import { json, mockApi } from "@/test/api"
import { fixtures } from "@/test/fixtures"
import { renderRoutes } from "@/test/routes"
import { ArtifactStoreItems } from "./artifact-store-items"

const darkmem = fixtures.artifactStore.repos[0] as ArtifactStoreRepo
const URL_1H = "https://s3.example.sg/gha-artifacts/darkraise/darkmem/102/e2e-report.zip?X-Amz-Expires=3600&X-Amz-Signature=a1"

function Items({ repo }: { repo: ArtifactStoreRepo }) {
  const [confirm, setConfirm] = useState<Confirm | null>(null)
  return (
    <>
      <ArtifactStoreItems repo={repo} offline={false} busy={false} ask={setConfirm} />
      <ConfirmDialog confirm={confirm} onClose={() => setConfirm(null)} />
    </>
  )
}

async function draw(repo: ArtifactStoreRepo = darkmem) {
  const view = renderRoutes({ "/": () => <Items repo={repo} />, "/actions/$repo/runs/$id": () => <p>run page</p> }, "/")
  // useNow reads the daemon's clock from the status, here 2026-10-03 14:05.
  view.queryClient.setQueryData(keys.status, fixtures.status)
  await screen.findByRole("region", { name: `Artifacts of ${repo.name}` })
  return view
}

const items = () => screen.getByRole("region", { name: "Artifacts of darkmem" })
const row = (name: RegExp) => within(items()).getByRole("row", { name })
const linkRoute = (url = URL_1H) => ({ "POST /api/artifact-store/link": { url, expires_at: "2026-10-03T15:05:00Z" } })
const posts = (calls: { method: string; path: string; body: unknown }[], path: string) =>
  calls.filter((c) => c.method === "POST" && c.path === path).map((c) => c.body)

afterEach(() => {
  Reflect.deleteProperty(navigator, "clipboard")
  Reflect.deleteProperty(window, "isSecureContext")
})

describe("ArtifactStoreItems", () => {
  it("lists each artifact with its run, branch, size and expiry", async () => {
    mockApi({})
    await draw()
    const e2e = row(/^e2e-report/)
    expect(within(e2e).getByRole("link", { name: "Run 102" })).toHaveAttribute("href", "/actions/darkmem/runs/102")
    for (const text of ["fix/cache-key", "8.9 MB", "2026-10-08"]) expect(within(e2e).getByText(text)).toBeInTheDocument()
    expect(within(row(/^coverage-report/)).getByText("2026-10-06")).toBeInTheDocument()
    expect(within(row(/Run 95/)).getByText("master")).toBeInTheDocument()
    expect(within(items()).queryByText(/Showing the newest/)).toBeNull()
  })

  it("downloads through a fresh one-hour link", async () => {
    const { calls } = mockApi(linkRoute())
    const { user } = await draw()
    const assign = vi.fn()
    vi.stubGlobal("location", { assign })
    await user.click(within(items()).getByRole("button", { name: "Download e2e-report from run 102" }))
    await waitFor(() => expect(assign).toHaveBeenCalledWith(URL_1H))
    expect(posts(calls, "/api/artifact-store/link")).toEqual([{ key: "darkraise/darkmem/102/e2e-report.zip", ttl: "1h" }])
  })

  it("copies a link valid for the chosen time", async () => {
    const url = "https://s3.example.sg/gha-artifacts/darkraise/darkmem/102/e2e-report.zip?X-Amz-Expires=86400&X-Amz-Signature=b2"
    const { calls } = mockApi(linkRoute(url))
    const { user } = await draw()
    Object.defineProperty(window, "isSecureContext", { value: true, configurable: true })
    const writeText = vi.fn(async () => {})
    Object.defineProperty(navigator, "clipboard", { value: { writeText }, configurable: true })
    await user.click(within(items()).getByRole("button", { name: "Copy a link to e2e-report from run 102" }))
    expect(await screen.findByText("Link valid for")).toBeInTheDocument()
    expect(screen.getByText("Anyone with the link can download it until it expires. The link goes straight to RustFS.")).toBeInTheDocument()
    expect(screen.getAllByRole("menuitem").map((m) => m.textContent)).toEqual(["1 hour", "24 hours", "7 days"])
    await user.click(screen.getByRole("menuitem", { name: "24 hours" }))
    await waitFor(() => expect(writeText).toHaveBeenCalledWith(url))
    expect(posts(calls, "/api/artifact-store/link")).toEqual([{ key: "darkraise/darkmem/102/e2e-report.zip", ttl: "24h" }])
    expect((await screen.findAllByText("Link copied · valid for 24 hours")).length).toBeGreaterThan(0)
  })

  it("deletes one artifact after asking", async () => {
    const { calls } = mockApi({ "POST /api/artifact-store/darkmem/artifacts/delete": () => json({ id: "op-3" }, 202) })
    const { user } = await draw()
    await user.click(within(items()).getByRole("button", { name: "Delete frontend-dist from run 95" }))
    const dialog = await screen.findByRole("alertdialog", { name: "Delete frontend-dist from run 95?" })
    expect(dialog).toHaveTextContent("Frees 2.4 MB on RustFS. A run that downloads it later fails.")
    expect(posts(calls, "/api/artifact-store/darkmem/artifacts/delete")).toEqual([])
    await user.click(within(dialog).getByRole("button", { name: "Delete" }))
    await waitFor(() => expect(posts(calls, "/api/artifact-store/darkmem/artifacts/delete")).toEqual([{ keys: ["darkraise/darkmem/95/frontend-dist.zip"] }]))
    expect((await screen.findAllByText("Delete started")).length).toBeGreaterThan(0)
  })

  it("deletes the listed artifacts older than an age after asking", async () => {
    const { calls } = mockApi({ "POST /api/artifact-store/darkmem/artifacts/delete": () => json({ id: "op-4" }, 202) })
    const { user } = await draw()
    await user.type(within(items()).getByRole("textbox", { name: "Delete artifacts of darkmem older than" }), "1h")
    await user.click(within(items()).getByRole("button", { name: "Delete older" }))
    const dialog = await screen.findByRole("alertdialog", { name: "Delete darkmem's artifacts older than 1h?" })
    expect(dialog).toHaveTextContent("2 artifacts (4.8 MB) on RustFS. A run that downloads them later fails.")
    await user.click(within(dialog).getByRole("button", { name: "Delete" }))
    await waitFor(() =>
      expect(posts(calls, "/api/artifact-store/darkmem/artifacts/delete")).toEqual([
        { keys: ["darkraise/darkmem/101/frontend-dist.zip", "darkraise/darkmem/95/frontend-dist.zip"] },
      ]),
    )
  })

  it.each<[string, string]>([
    ["30d", "No artifact of darkmem is older than 30d"],
    ["soon", "Enter an age such as 7d or 12h"],
  ])("sends nothing for the age %s", async (age, message) => {
    const { calls } = mockApi({})
    const { user } = await draw()
    await user.type(within(items()).getByRole("textbox", { name: "Delete artifacts of darkmem older than" }), age)
    await user.click(within(items()).getByRole("button", { name: "Delete older" }))
    expect((await screen.findAllByText(message)).length).toBeGreaterThan(0)
    expect(screen.queryByRole("alertdialog")).toBeNull()
    expect(calls.filter((c) => c.method === "POST")).toHaveLength(0)
  })

  it("says when it lists only the newest artifacts", async () => {
    mockApi({})
    await draw({ ...darkmem, count: 742 })
    expect(within(items()).getByText("Showing the newest 4 of 742.")).toBeInTheDocument()
  })

  it("says when a repository has none", async () => {
    mockApi({})
    await draw({ ...darkmem, name: "docs", items: [], count: 0, bytes: 0 })
    expect(within(screen.getByRole("region", { name: "Artifacts of docs" })).getByText("No artifacts on RustFS.")).toBeInTheDocument()
  })
})
