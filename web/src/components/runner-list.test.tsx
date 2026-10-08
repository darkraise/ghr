import { useParams } from "@tanstack/react-router"
import { screen, waitFor } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import type { Status } from "@/api/types"
import { fixtures } from "@/test/fixtures"
import { renderRoutes } from "@/test/routes"
import { RunnerList } from "./runner-list"

const now = Date.parse("2026-10-03T14:05:00Z")

function ListAt({ status }: { status: Status | undefined }) {
  const { id } = useParams({ strict: false })
  return <RunnerList status={status} selected={id} tab="steps" now={now} />
}

// null is "no status yet": passing undefined would pick the default.
function draw(status: Status | null = fixtures.status, path = "/runners/aaaaaa?tab=steps") {
  return renderRoutes(
    {
      "/runners": () => <ListAt status={status ?? undefined} />,
      "/runners/$id": () => <ListAt status={status ?? undefined} />,
      "/repositories": () => <p>repositories page</p>,
    },
    path,
  )
}

const link = (id: string) => screen.findByRole("link", { name: new RegExp(`^${id}`) })

describe("RunnerList", () => {
  it("shows each runner's state, repository, job and elapsed time", async () => {
    draw()
    const busy = await link("aaaaaa")
    expect(busy).toHaveTextContent("darkmem")
    expect(busy).toHaveTextContent("test #42")
    expect(busy).toHaveTextContent("4m00s")
    expect(busy).toHaveTextContent("Busy")
    expect(await link("bbbbbb")).toHaveTextContent("Idle")
  })

  it("marks the selected runner and makes only it tabbable", async () => {
    draw()
    const selected = await link("aaaaaa")
    expect(selected).toHaveAttribute("aria-current", "page")
    expect(selected).toHaveAttribute("tabindex", "0")
    expect(await link("bbbbbb")).toHaveAttribute("tabindex", "-1")
  })

  it("moves with the arrow, Home and End keys without adding history", async () => {
    const { router, user } = draw()
    const first = await link("aaaaaa")
    first.focus()
    const depth = router.history.length
    await user.keyboard("{ArrowDown}")
    await waitFor(() => expect(router.state.location.pathname).toBe("/runners/bbbbbb"))
    expect(document.activeElement).toBe(await link("bbbbbb"))
    expect(router.history.length).toBe(depth)
    await user.keyboard("{Home}")
    await waitFor(() => expect(router.state.location.pathname).toBe("/runners/aaaaaa"))
    await user.keyboard("{End}")
    await waitFor(() => expect(router.state.location.pathname).toBe("/runners/bbbbbb"))
  })

  it("lists repositories waiting at their cap", async () => {
    const repos = [{ name: "darkrouter", paused: false, max: 2, active: 2, queued: 2 }]
    draw({ ...fixtures.status, repos })
    expect(await screen.findByText("darkrouter, 2 queued, cap 2")).toBeInTheDocument()
    expect(screen.getByRole("heading", { name: "Waiting" })).toBeInTheDocument()
  })

  it("says when no runner is up and links to the repositories", async () => {
    draw({ ...fixtures.status, instances: [], repos: [] }, "/runners")
    expect(await screen.findByText("No runners. They start when a job is queued.")).toBeInTheDocument()
    expect(screen.getByRole("link", { name: "Repositories" })).toHaveAttribute("href", "/repositories")
  })

  it("waits for the first status", async () => {
    draw(null, "/runners")
    expect(await screen.findByText("Waiting for the daemon")).toBeInTheDocument()
  })
})
