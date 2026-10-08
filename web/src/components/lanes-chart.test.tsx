import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { describe, expect, it, vi } from "vitest"
import type { Activity } from "@/api/types"
import { fixtures } from "@/test/fixtures"
import { LanesChart } from "./lanes-chart"

const now = Date.parse("2026-10-03T14:05:00Z")

function draw(over: Partial<Activity> = {}, at = now) {
  const onOpenRunner = vi.fn()
  const view = render(<LanesChart activity={{ ...fixtures.activityLanes, ...over }} now={at} width={960} onOpenRunner={onOpenRunner} />)
  return { ...view, onOpenRunner }
}

const runningWidth = (container: HTMLElement) => Number(container.querySelector('rect[data-state="running"]')?.getAttribute("width"))

describe("LanesChart", () => {
  it("labels each lane and the time axis", () => {
    draw()
    expect(screen.getByText("Lane 1")).toBeInTheDocument()
    expect(screen.getByText("Lane 2")).toBeInTheDocument()
    expect(screen.getByText("13:10")).toBeInTheDocument()
    expect(screen.getByText("14:00")).toBeInTheDocument()
  })

  it("draws each segment state", () => {
    const extra: Activity["lanes"][number] = {
      runs: [
        { instance_id: "cccccc", repo: "darkmem", segments: [{ state: "starting", from: "2026-10-03T13:06:00Z", to: "2026-10-03T13:07:00Z" }] },
        { instance_id: "dddddd", repo: "darkmem", job: "docs", segments: [{ state: "cancelled", from: "2026-10-03T13:08:00Z", to: "2026-10-03T13:09:00Z" }] },
        { instance_id: "eeeeee", repo: "darkmem", job: "deploy", segments: [{ state: "skipped", from: "2026-10-03T13:10:00Z", to: "2026-10-03T13:11:00Z" }] },
      ],
    }
    const { container } = draw({ lanes: [...fixtures.activityLanes.lanes, extra] })
    for (const state of ["failed", "succeeded", "running", "warm", "starting", "cancelled", "skipped"]) {
      expect(container.querySelector(`rect[data-state="${state}"]`)).not.toBeNull()
    }
    expect(container.querySelector('rect[data-state="warm"]')).toHaveAttribute("stroke-dasharray", "3 2")
    expect(container.querySelector(".ghr-pulse")).not.toBeNull()
  })

  it("links a finished job to GitHub", () => {
    draw()
    const link = screen.getByRole("link", { name: "darkmem, ci, build, run 41, succeeded, 5m00s" })
    expect(link).toHaveAttribute("href", "https://github.com/darkraise/darkmem/actions/runs/101/job/1")
    expect(link).toHaveAttribute("target", "_blank")
  })

  it("opens the runner page from a live run", async () => {
    const { onOpenRunner } = draw()
    await userEvent.setup().click(screen.getByRole("link", { name: "darkmem, ci, test, run 42, running, 4m00s" }))
    expect(onOpenRunner).toHaveBeenCalledWith("aaaaaa")
  })

  it("keeps a finished run without a GitHub link focusable but not a link", () => {
    draw()
    const name = "darkmem, ci, lint, run 40, failed, 6m00s"
    expect(screen.getByRole("img", { name })).toHaveAttribute("tabindex", "0")
    expect(screen.queryByRole("link", { name })).toBeNull()
  })

  it("extends a running bar to now", () => {
    const { container, rerender } = draw()
    const before = runningWidth(container)
    rerender(<LanesChart activity={fixtures.activityLanes} now={now + 60_000} width={960} onOpenRunner={vi.fn()} />)
    expect(runningWidth(container)).toBeGreaterThan(before)
  })

  it("draws waiting jobs, CPU and a dashed memory line, with gaps", () => {
    const { container } = draw()
    expect(container.querySelector('path[data-track="waiting"]')).not.toBeNull()
    expect(container.querySelectorAll('polyline[data-track="cpu"]')).toHaveLength(1)
    expect(container.querySelector('polyline[data-track="mem"]')).toHaveAttribute("stroke-dasharray", "3 2")
    expect(screen.getByText("peak 3")).toBeInTheDocument()
    expect(screen.getByText("peak 48%")).toBeInTheDocument()
  })

  it("says when no jobs ran", () => {
    draw({ lanes: [] })
    expect(screen.getByText("No jobs ran in the last hour.")).toBeInTheDocument()
    expect(screen.getByText("Lane 1")).toBeInTheDocument()
  })

  it("says no jobs ran when only a warm runner is drawn", () => {
    draw({ lanes: [{ runs: [{ instance_id: "ffffff", repo: "darkmem", segments: [{ state: "warm", from: "2026-10-03T13:10:00Z", to: null }] }] }] })
    expect(screen.getByText("No jobs ran in the last hour.")).toBeInTheDocument()
  })

  it("does not link a run whose URL is not https", () => {
    const lanes = fixtures.activityLanes.lanes.map((l) => ({ runs: l.runs.map((r) => (r.html_url ? { ...r, html_url: "javascript:void(0)" } : r)) }))
    draw({ lanes })
    const name = "darkmem, ci, build, run 41, succeeded, 5m00s"
    expect(screen.queryByRole("link", { name })).toBeNull()
    expect(screen.getByRole("img", { name })).toHaveAttribute("tabindex", "0")
  })

  it("drops the retention note when the shaded strip cannot hold it", () => {
    const { container } = draw({ history_from: "2026-10-03T13:06:00Z" })
    expect(container.querySelector('[data-retention="true"]')).not.toBeNull()
    expect(screen.queryByText(/^History is kept for/)).toBeNull()
  })

  it("shades the time before history starts", () => {
    const { container } = draw({ history_from: "2026-10-03T13:35:00Z" })
    expect(container.querySelector('[data-retention="true"]')).not.toBeNull()
    expect(screen.getByText(/^History is kept for/)).toBeInTheDocument()
  })

  it("marks now", () => {
    const { container } = draw()
    expect(container.querySelector('line[data-now="true"]')).toHaveAttribute("x1", String(960 - 8))
  })
})
