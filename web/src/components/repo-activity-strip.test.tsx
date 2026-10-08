import { render, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import type { ActivityHour } from "@/api/types"
import { RepoActivityStrip } from "./repo-activity-strip"

const hour = (i: number, over: Partial<ActivityHour> = {}): ActivityHour => ({
  start: new Date(Date.parse("2026-10-02T15:00:00Z") + i * 3_600_000).toISOString(),
  succeeded: 0,
  failed: 0,
  cancelled: 0,
  ...over,
})

describe("RepoActivityStrip", () => {
  it("shades each hour by its runs and outlines the hour in progress", () => {
    const hours = Array.from({ length: 24 }, (_, i) => hour(i))
    hours[5] = hour(5, { succeeded: 1 })
    hours[6] = hour(6, { succeeded: 3 })
    hours[7] = hour(7, { succeeded: 5 })
    hours[8] = hour(8, { succeeded: 2, failed: 1 })
    hours[9] = hour(9, { cancelled: 1 })
    render(<RepoActivityStrip repo="darkmem" hours={hours} />)
    const strip = screen.getByRole("img", { name: "darkmem, last 24 hours: 11 succeeded, 1 failed" })
    const cells = [...strip.children]
    expect(cells.map((c) => c.getAttribute("data-cell")).slice(4, 10)).toEqual(["none", "ok-1", "ok-2", "ok-3", "bad", "ok-1"])
    expect(cells[23]).toHaveAttribute("data-open", "true")
    expect(cells[23]).toHaveClass("outline-primary")
    expect(cells[22]).not.toHaveAttribute("data-open")
  })

  it("says when nothing ran", () => {
    render(<RepoActivityStrip repo="darkmem" hours={Array.from({ length: 24 }, (_, i) => hour(i))} />)
    expect(screen.getByRole("img", { name: "darkmem, last 24 hours: no runs" })).toBeInTheDocument()
  })
})
