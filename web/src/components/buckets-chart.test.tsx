import { fireEvent, render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { describe, expect, it, vi } from "vitest"
import type { Activity, ActivityBucket } from "@/api/types"
import { fixtures } from "@/test/fixtures"
import { BucketsChart } from "./buckets-chart"

const now = Date.parse("2026-10-03T14:05:00Z")
const base = fixtures.activityBuckets

function draw(over: Partial<Activity> = {}, props: Partial<Parameters<typeof BucketsChart>[0]> = {}) {
  return render(<BucketsChart activity={{ ...base, ...over }} now={now} width={960} {...props} />)
}

describe("BucketsChart", () => {
  it("labels each track with the peak it reaches", () => {
    draw()
    for (const track of ["Busy", "Runs", "Waiting", "CPU"]) expect(screen.getByText(track)).toBeInTheDocument()
    expect(screen.getByText("peak 10 min")).toBeInTheDocument()
    expect(screen.getByText("peak 1")).toBeInTheDocument()
    expect(screen.getByText("peak 3")).toBeInTheDocument()
    expect(screen.getByText("peak 23%")).toBeInTheDocument()
  })

  it("measures busy time against capacity when there is one", () => {
    const buckets = base.buckets.map((b) => ({ ...b, busy_pct: (b.busy_minutes / 120) * 100 }))
    draw({ capacity: 2, buckets })
    expect(screen.getByText("peak 8%")).toBeInTheDocument()
  })

  it("makes every column focusable with its numbers", () => {
    draw()
    expect(screen.getAllByRole("img")).toHaveLength(25)
    const column = screen.getByRole("img", {
      name: "13:00 to 14:00, 5 busy runner-minutes, 1 succeeded, 0 failed, 0 cancelled, at most 3 waiting, CPU 23%",
    })
    expect(column).toHaveAttribute("tabindex", "0")
  })

  it("draws the bucket in progress lighter", () => {
    const { container } = draw()
    const open = container.querySelectorAll('[data-open="true"]')
    expect(open).toHaveLength(1)
    expect(open[0]).toHaveClass("opacity-60")
  })

  it("stacks failed runs on succeeded runs", () => {
    const { container } = draw()
    expect(container.querySelectorAll('rect[data-kind="succeeded"]')).toHaveLength(1)
    expect(container.querySelectorAll('rect[data-kind="failed"]')).toHaveLength(1)
  })

  it("leaves gaps for missing metrics and says when they start", () => {
    const { container } = draw()
    expect(screen.getAllByText("Collecting data since 13:00")).toHaveLength(2)
    expect(container.querySelectorAll('circle[data-track="waiting"]')).toHaveLength(1)
    expect(container.querySelectorAll('polyline[data-track="waiting"]')).toHaveLength(0)
  })

  it("shades the time before history starts", () => {
    const { container } = draw({ history_from: "2026-10-03T00:05:00Z" })
    expect(container.querySelector('[data-retention="true"]')).not.toBeNull()
    expect(screen.getByText("History is kept for 14 hours")).toBeInTheDocument()
  })

  it("drops the retention note when the shaded strip cannot hold it", () => {
    const { container } = draw({ history_from: "2026-10-02T14:10:00Z" })
    expect(container.querySelector('[data-retention="true"]')).not.toBeNull()
    expect(screen.queryByText(/^History is kept for/)).toBeNull()
  })

  it("labels time every 6 hours", () => {
    draw()
    expect(screen.getByText("18:00")).toBeInTheDocument()
    expect(screen.getByText("00:00")).toBeInTheDocument()
    expect(screen.queryByText("19:00")).toBeNull()
  })

  it("marks now inside the bucket in progress", () => {
    const { container } = draw()
    const x = Number(container.querySelector('line[data-now="true"]')?.getAttribute("x1"))
    const colW = (960 - 64 - 8) / 25
    expect(x).toBeGreaterThan(64 + 24 * colW)
    expect(x).toBeLessThan(64 + 25 * colW)
  })

  it("says when no jobs ran", () => {
    draw({ buckets: base.buckets.map((b) => ({ ...b, succeeded: 0, failed: 0, cancelled: 0 })) })
    expect(screen.getByText("No jobs ran in the last 24 hours.")).toBeInTheDocument()
  })

  it("draws only busy time and finished jobs in jobs mode", () => {
    const { container } = draw({}, { tracks: "jobs" })
    expect(screen.getByRole("group", { name: "Jobs per bucket for the last 24 hours" })).toBeInTheDocument()
    expect(screen.getByText("Busy")).toBeInTheDocument()
    expect(screen.getByText("Runs")).toBeInTheDocument()
    expect(screen.queryByText("Waiting")).toBeNull()
    expect(screen.queryByText("CPU")).toBeNull()
    expect(container.querySelectorAll("[data-track]")).toHaveLength(0)
    expect(screen.queryByText(/^Collecting data since/)).toBeNull()
  })

  it("lets a bucket be picked by click, Enter or Space when asked", async () => {
    const onPick = vi.fn<(b: ActivityBucket) => void>()
    draw({}, { tracks: "jobs", onPick, picked: "2026-10-03T13:00:00Z" })
    const buttons = screen.getAllByRole("button")
    expect(buttons).toHaveLength(25)
    const thirteen = screen.getByRole("button", { name: /^13:00 to 14:00/ })
    expect(thirteen).toHaveAttribute("aria-pressed", "true")
    expect(buttons.filter((b) => b.getAttribute("aria-pressed") === "true")).toHaveLength(1)
    const user = userEvent.setup()
    await user.click(thirteen)
    thirteen.focus()
    await user.keyboard("{Enter}")
    await user.keyboard(" ")
    expect(onPick).toHaveBeenCalledTimes(3)
    expect(onPick.mock.calls[0]?.[0].start).toBe("2026-10-03T13:00:00Z")
  })

  it("ignores the repeats of a held key", () => {
    const onPick = vi.fn<(b: ActivityBucket) => void>()
    draw({}, { tracks: "jobs", onPick })
    const first = screen.getAllByRole("button")[0] as HTMLElement
    fireEvent.keyDown(first, { key: "Enter" })
    fireEvent.keyDown(first, { key: "Enter", repeat: true })
    fireEvent.keyDown(first, { key: " ", repeat: true })
    expect(onPick).toHaveBeenCalledTimes(1)
  })

  it("keeps image buckets without onPick", () => {
    draw()
    expect(screen.queryAllByRole("button")).toHaveLength(0)
    expect(screen.getAllByRole("img")).toHaveLength(25)
  })
})
