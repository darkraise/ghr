import { fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { TooltipProvider } from "darkraise-ui/components/tooltip"
import { describe, expect, it, vi } from "vitest"
import type { RepoStatus } from "@/api/types"
import { mockApi, noContent } from "@/test/api"
import { fixtures } from "@/test/fixtures"
import { withQuery } from "@/test/query"
import { RepoTable } from "./repo-table"

const now = Date.parse("2026-10-03T14:05:00Z")
const [darkmem, darkcloud, oldRepo] = fixtures.status.repos as [RepoStatus, RepoStatus, RepoStatus]

function draw(repos: RepoStatus[] = fixtures.status.repos, offline = false) {
  const onOpen = vi.fn()
  const onEdit = vi.fn()
  const { wrapper } = withQuery()
  const user = userEvent.setup()
  render(
    <TooltipProvider>
      <RepoTable repos={repos} activity={fixtures.activityLanes.repos} now={now} offline={offline} onOpen={onOpen} onEdit={onEdit} />
    </TooltipProvider>,
    { wrapper },
  )
  return { onOpen, onEdit, user }
}

const row = (name: string) => screen.getByRole("link", { name }).closest("tr") as HTMLElement
const cell = (name: string, index: number) => row(name).querySelectorAll("td")[index] as HTMLElement

describe("RepoTable", () => {
  it("names each state in a word", () => {
    draw([darkmem, darkcloud, oldRepo, { ...darkmem, name: "idle-repo", active: 0 }, { ...darkcloud, name: "leaving", paused: false, removing: true }])
    const pill = (name: string, word: string, fill: string, text: string) => {
      const label = within(row(name)).getByText(word)
      expect(label.parentElement).toHaveClass("rounded-full", fill)
      expect(label.parentElement?.querySelector("svg")).toHaveClass(text)
    }
    pill("darkmem", "Running", "bg-running/15", "text-running")
    pill("darkcloud", "Paused", "bg-muted-foreground/15", "text-muted-foreground")
    pill("old-repo", "Error", "bg-destructive/15", "text-destructive")
    pill("idle-repo", "Idle", "bg-muted-foreground/15", "text-muted-foreground")
    pill("leaving", "Removing", "bg-muted-foreground/15", "text-muted-foreground")
  })

  it("shows a repository's raw error under its name", () => {
    draw()
    expect(within(row("old-repo")).getByText("GitHub: not found")).toHaveClass("text-destructive")
  })

  it("shows waiting jobs in warn and leaves zero blank", () => {
    draw()
    expect(within(row("darkmem")).getByText("3")).toHaveClass("text-warning")
    expect(cell("darkcloud", 3).textContent).toBe("")
  })

  it("draws one cell per allowed runner, or only the count when unlimited", () => {
    draw([darkmem, { ...darkcloud, max: 0, active: 1 }])
    expect(cell("darkmem", 2)).toHaveTextContent("1/2")
    expect(cell("darkmem", 2).querySelectorAll('[data-slot="runner"]')).toHaveLength(2)
    expect(cell("darkmem", 2).querySelectorAll('[data-filled="true"]')).toHaveLength(1)
    expect(cell("darkcloud", 2).textContent).toBe("1")
  })

  it("draws one track instead of a cell per runner when there are many", () => {
    draw([{ ...darkmem, max: 99, active: 33 }])
    const meter = cell("darkmem", 2)
    expect(meter).toHaveTextContent("33/99")
    expect(meter.querySelectorAll('[data-slot="runner"]')).toHaveLength(0)
    const track = meter.querySelector('[data-slot="runner-track"]')
    expect(track).toBeInTheDocument()
    expect(parseFloat((track?.firstElementChild as HTMLElement).style.width)).toBeCloseTo(33.33, 1)
  })

  it("keeps a lone busy runner visible on the track", () => {
    draw([{ ...darkmem, max: 99, active: 1 }])
    expect(cell("darkmem", 2).querySelector('[data-slot="runner-track"]')?.firstElementChild).toHaveClass("min-w-px")
  })

  it("leaves the track empty when no runner is busy", () => {
    draw([{ ...darkmem, max: 99, active: 0 }])
    expect(cell("darkmem", 2).querySelector('[data-slot="runner-track"]')?.firstElementChild).toBeNull()
  })

  it("draws the last 24 hours with the hour in progress outlined", () => {
    draw()
    const strip = within(row("darkmem")).getByRole("img", { name: /^darkmem, last 24 hours/ })
    expect(strip.children).toHaveLength(24)
    expect(strip.lastElementChild).toHaveAttribute("data-open", "true")
  })

  it("shows the last job with its result", () => {
    draw()
    const last = cell("darkmem", 5)
    expect(within(last).getByRole("img", { name: "Succeeded" })).toBeInTheDocument()
    expect(last).toHaveTextContent("build #41")
    expect(last).toHaveTextContent("25m ago")
  })

  it("pauses from the row menu", async () => {
    const { calls } = mockApi({ "POST /api/repos/darkmem/pause": () => noContent() })
    const { user } = draw()
    await user.click(screen.getByRole("button", { name: "More actions for darkmem" }))
    await user.click(await screen.findByRole("menuitem", { name: "Pause" }))
    await waitFor(() => expect(calls.some((c) => c.method === "POST" && c.path === "/api/repos/darkmem/pause")).toBe(true))
  })

  it("asks before removing from the row menu", async () => {
    const { calls } = mockApi({ "DELETE /api/repos/darkmem": () => noContent() })
    const { user } = draw()
    await user.click(screen.getByRole("button", { name: "More actions for darkmem" }))
    await user.click(await screen.findByRole("menuitem", { name: "Remove" }))
    const ask = within(await screen.findByRole("alertdialog"))
    expect(calls).toHaveLength(0)
    await user.click(ask.getByRole("button", { name: "Remove" }))
    await waitFor(() => expect(calls.some((c) => c.method === "DELETE" && c.path === "/api/repos/darkmem")).toBe(true))
  })

  it("opens a repository from its name, its row or Edit", async () => {
    const { onOpen, onEdit, user } = draw()
    await user.click(screen.getByRole("link", { name: "darkcloud" }))
    expect(onOpen).toHaveBeenLastCalledWith("darkcloud")
    await user.click(within(row("old-repo")).getByText("Error"))
    expect(onOpen).toHaveBeenLastCalledWith("old-repo")
    await user.click(screen.getByRole("button", { name: "More actions for darkmem" }))
    await user.click(await screen.findByRole("menuitem", { name: "Edit" }))
    expect(onEdit).toHaveBeenCalledWith("darkmem")
    expect(onOpen).toHaveBeenCalledTimes(2)
  })

  it.each([
    ["Ctrl", { ctrlKey: true }],
    ["Cmd", { metaKey: true }],
    ["Shift", { shiftKey: true }],
    ["Alt", { altKey: true }],
  ])("leaves a click with %s held on a repository's name to the browser", (_, modifier) => {
    const { onOpen } = draw()
    const proceeded = fireEvent.click(screen.getByRole("link", { name: "darkcloud" }), modifier)
    expect(proceeded).toBe(true)
    expect(onOpen).not.toHaveBeenCalled()
  })

  it("locks pause and remove while the daemon is unreachable", async () => {
    const { calls } = mockApi({})
    const { user } = draw(fixtures.status.repos, true)
    await user.click(screen.getByRole("button", { name: "More actions for darkmem" }))
    expect(await screen.findByRole("menuitem", { name: "Pause" })).toHaveAttribute("aria-disabled", "true")
    expect(screen.getByRole("menuitem", { name: "Remove" })).toHaveAttribute("aria-disabled", "true")
    expect(screen.getByRole("menuitem", { name: "Edit" })).not.toHaveAttribute("aria-disabled", "true")
    await user.click(screen.getByRole("menuitem", { name: "Pause" }))
    expect(calls).toHaveLength(0)
  })

  it("names the row menu in a tooltip", async () => {
    const { user } = draw()
    await user.hover(screen.getByRole("button", { name: "More actions for darkmem" }))
    expect(await screen.findByRole("tooltip", {}, { timeout: 3000 })).toHaveTextContent("More actions")
  })
})
