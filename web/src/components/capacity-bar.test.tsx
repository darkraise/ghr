import { render, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import type { InstanceStatus, Status } from "@/api/types"
import { capacity } from "@/lib/capacity"
import { fixtures } from "@/test/fixtures"
import { CapacityBar, CapacitySegments } from "./capacity-bar"

const inst = (state: string, id: string): InstanceStatus => ({
  id,
  repo: "darkmem",
  runner_name: `ghr-${id}`,
  state,
  since: "2026-10-03T14:00:00Z",
})
const status = (over: Partial<Status>): Status => ({ ...fixtures.status, ...over })
const threeBusy = status({ global_max: 2, instances: [inst("busy", "a"), inst("busy", "b"), inst("busy", "c")] })

describe("capacity", () => {
  it.each([
    [
      "queue mode",
      status({ global_max: 4, instances: [inst("busy", "a"), inst("busy", "b"), inst("idle", "c")] }),
      ["busy", "busy", "warm", "free"],
      "2 of 4 busy",
      "2 of 4 busy, 1 warm, 1 free",
    ],
    [
      "starting, with cleaning left out",
      status({ global_max: 4, instances: [inst("starting", "a"), inst("cleaning", "b"), inst("busy", "c")] }),
      ["busy", "starting", "free", "free"],
      "1 of 4 busy",
      "1 of 4 busy, 1 starting, 2 free",
    ],
    ["above the max", threeBusy, ["busy", "busy", "busy"], "3 busy, max 2", "3 busy, max 2"],
    [
      "all mode",
      status({ mode: "all", instances: [inst("busy", "a"), inst("idle", "b")] }),
      ["busy", "warm"],
      "1 busy",
      "1 busy, 1 warm",
    ],
  ])("%s", (_name, st, segments, text, label) => {
    const c = capacity(st)
    expect(c.segments).toEqual(segments)
    expect(c.text).toBe(text)
    expect(c.label).toBe(label)
  })

  it("counts the segments above the max", () => {
    expect(capacity(threeBusy).over).toBe(1)
    expect(capacity(fixtures.status).over).toBe(0)
  })
})

describe("CapacityBar", () => {
  it("draws one segment per runner and free slot, with the count", () => {
    render(<CapacityBar status={fixtures.status} />)
    expect(screen.getByText("Runners")).toBeInTheDocument()
    expect(screen.getByText("1 of 2 busy")).toHaveClass("font-mono")
    const bar = screen.getByRole("img", { name: "1 of 2 busy, 1 warm" })
    expect([...bar.children].map((c) => c.getAttribute("data-kind"))).toEqual(["busy", "warm"])
    expect(bar.children[0]).toHaveClass("ghr-pulse")
    expect(bar.children[1]).not.toHaveClass("ghr-pulse")
  })

  it("sets the segments above the max apart", () => {
    const { container } = render(<CapacitySegments status={threeBusy} />)
    const segments = container.querySelectorAll("[data-kind]")
    expect(segments[2]).toHaveClass("ml-[2px]")
    expect(segments[1]).not.toHaveClass("ml-[2px]")
  })

  it("draws free slots in the shell colour on the rail and in sunk on a card", () => {
    const free = status({ global_max: 3, instances: [inst("busy", "a")] })
    const { container, rerender } = render(<CapacitySegments status={free} tone="rail" />)
    expect(container.querySelector('[data-kind="free"]')).toHaveClass("bg-[hsl(var(--surface-sidebar))]")
    rerender(<CapacitySegments status={free} />)
    expect(container.querySelector('[data-kind="free"]')).toHaveClass("bg-muted")
  })
})
