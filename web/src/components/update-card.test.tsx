import { render, screen, waitFor } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { describe, expect, it } from "vitest"
import type { RunnerUpdate } from "@/api/types"
import { DAY_MS } from "@/lib/duration"
import { monthDay } from "@/lib/format"
import { mockApi, noContent } from "@/test/api"
import { withQuery } from "@/test/query"
import { UpdateCard } from "./update-card"

function draw(update: RunnerUpdate, offline = false) {
  const { wrapper } = withQuery()
  const user = userEvent.setup()
  const view = render(<UpdateCard update={update} offline={offline} />, { wrapper })
  return { ...view, user }
}

// An hour past whole days, so the day count does not tip while the test runs.
const inDays = (days: number) => new Date(Date.now() + days * DAY_MS + 3_600_000).toISOString()

describe("UpdateCard", () => {
  it("stays hidden when there is nothing to do", () => {
    const { container } = draw({ installed: "2.338.0", latest: "2.338.0" })
    expect(container).toBeEmptyDOMElement()
  })

  it("asks to queue an update before the deadline", async () => {
    const { calls } = mockApi({ "POST /api/runner-update": () => noContent() })
    const deadline = inDays(20)
    const { user } = draw({ latest: "2.338.0", deadline })
    expect(screen.getByText(`2.338.0 required by ${monthDay(new Date(deadline))}, 20 days left`)).toBeInTheDocument()
    expect(screen.getByRole("heading", { name: "Runner update" })).toHaveClass("text-warning")
    await user.click(screen.getByRole("button", { name: "Queue update" }))
    await waitFor(() => expect(calls.some((c) => c.method === "POST" && c.path === "/api/runner-update")).toBe(true))
  })

  it("turns red within 7 days of the deadline", () => {
    draw({ latest: "2.338.0", deadline: inDays(6) })
    expect(screen.getByText(/, 6 days left$/)).toBeInTheDocument()
    expect(screen.getByRole("heading", { name: "Runner update" })).toHaveClass("text-destructive")
  })

  it("says when the deadline has passed", () => {
    draw({ latest: "2.338.0", deadline: inDays(-2) })
    expect(screen.getByText(/, overdue$/)).toBeInTheDocument()
  })

  it("offers to cancel a queued update", async () => {
    const { calls } = mockApi({ "DELETE /api/runner-update": () => noContent() })
    const { user } = draw({ latest: "2.338.0", queued: true, queued_at: "2026-10-03T14:02:00Z" })
    expect(screen.getByText("Queued since 14:02. Runners update between jobs.")).toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: "Cancel update" }))
    await waitFor(() => expect(calls.some((c) => c.method === "DELETE" && c.path === "/api/runner-update")).toBe(true))
  })

  it("shows a running update with no action", () => {
    draw({ running: true, queued: true })
    expect(screen.getByText("Updating runners")).toBeInTheDocument()
    expect(screen.queryByRole("button")).toBeNull()
  })

  it("offers to try a failed update again", async () => {
    const { calls } = mockApi({ "POST /api/runner-update": () => noContent() })
    const { user } = draw({ latest: "2.338.0", deadline: inDays(20), last_outcome: "failed", last_error: "download failed: 502" })
    expect(screen.getByText("download failed: 502")).toHaveClass("text-destructive")
    await user.click(screen.getByRole("button", { name: "Try again" }))
    await waitFor(() => expect(calls.some((c) => c.method === "POST")).toBe(true))
  })

  it("disables its action while the daemon is unreachable", () => {
    draw({ latest: "2.338.0", deadline: inDays(20) }, true)
    expect(screen.getByRole("button", { name: "Queue update" })).toBeDisabled()
  })
})
