import { screen, within } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"
import type { RepoStatus } from "@/api/types"
import { mockApi } from "@/test/api"
import { fixtures } from "@/test/fixtures"
import { renderRoutes } from "@/test/routes"
import { RepoList } from "./repo-list"

const [first] = fixtures.status.repos as [RepoStatus, ...RepoStatus[]]

function draw(repos: RepoStatus[] = fixtures.status.repos, mode = fixtures.status.mode, onEdit = vi.fn()) {
  mockApi({})
  const view = renderRoutes(
    {
      "/": () => (
        <RepoList repos={repos} config={{ ...fixtures.config, mode }} activity={fixtures.activityBuckets.repos} offline={false} onEdit={onEdit} />
      ),
      "/repositories/$name": () => <p>overview</p>,
    },
    "/",
  )
  return { ...view, onEdit }
}

const row = async (name: string) => within((await screen.findByRole("link", { name })).closest("li") as HTMLElement)

describe("RepoList", () => {
  it("shows one row per repository, with its name as a link and its state as a pill", async () => {
    draw()
    const repo = first
    const link = await screen.findByRole("link", { name: repo.name })
    expect(link).toHaveAttribute("href", `/repositories/${repo.name}`)
    expect(screen.getAllByRole("listitem")).toHaveLength(fixtures.status.repos.length)
    expect((link.closest("li") as HTMLElement).querySelector("span[class*='rounded-full']")).not.toBeNull()
  })

  it("keeps a repository's error text under its name", async () => {
    const broken = { ...first, error: "PAT cannot read this repository" }
    draw([broken])
    expect((await row(broken.name)).getByText("PAT cannot read this repository")).toHaveClass("text-destructive")
  })

  it("says a repository is being removed in place of its facts", async () => {
    const leaving = { ...first, removing: true }
    draw([leaving])
    expect((await row(leaving.name)).getByText("Removing. Running jobs finish first.")).toBeInTheDocument()
  })

  it("shows the warm count in all mode only", async () => {
    const repo = first
    const view = draw(undefined, "all")
    expect((await row(repo.name)).getByText(/^Warm \d+$/)).toBeInTheDocument()
    view.unmount()
    draw(undefined, "queue")
    expect((await row(repo.name)).queryByText(/^Warm \d+$/)).not.toBeInTheDocument()
  })

  it("sends Edit to onEdit", async () => {
    const repo = first
    const { user, onEdit } = draw()
    await user.click((await row(repo.name)).getByRole("button", { name: `More actions for ${repo.name}` }))
    await user.click(await screen.findByRole("menuitem", { name: "Edit" }))
    expect(onEdit).toHaveBeenCalledWith(repo.name)
  })

  it("stacks the right-hand group under the facts below 768px", async () => {
    draw()
    const item = (await screen.findByRole("link", { name: first.name })).closest("li") as HTMLElement
    expect(item).toHaveClass("flex-col", "md:flex-row")
  })
})
