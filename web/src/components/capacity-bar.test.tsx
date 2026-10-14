import { render, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import type { InstanceStatus, Status } from "@/api/types"
import { capacity } from "@/lib/capacity"
import { fixtures } from "@/test/fixtures"
import { CapacitySegments } from "./capacity-bar"

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

describe("CapacitySegments", () => {
  it("draws one segment per runner and free slot", () => {
    render(<CapacitySegments status={fixtures.status} />)
    const bar = screen.getByRole("img", { name: "1 of 2 busy, 1 warm" })
    expect([...bar.children].map((c) => c.getAttribute("data-kind"))).toEqual(["busy", "warm"])
    expect(bar.children[0]).toHaveClass("ghr-pulse", "bg-running")
    expect(bar.children[1]).not.toHaveClass("ghr-pulse")
    expect(bar.children[1]).toHaveClass("border-running")
  })

  it("sets the segments above the max apart", () => {
    const { container } = render(<CapacitySegments status={threeBusy} />)
    const segments = container.querySelectorAll("[data-kind]")
    expect(segments[2]).toHaveClass("ml-[2px]")
    expect(segments[1]).not.toHaveClass("ml-[2px]")
  })

  it("draws free slots in the sunk colour", () => {
    const free = status({ global_max: 3, instances: [inst("busy", "a")] })
    const { container } = render(<CapacitySegments status={free} />)
    expect(container.querySelector('[data-kind="free"]')).toHaveClass("bg-muted")
  })

  it("groups the segments of one kind once there are too many to tell apart", () => {
    const many = status({
      global_max: 99,
      instances: [inst("busy", "a"), inst("busy", "b"), inst("busy", "c"), inst("starting", "d"), inst("idle", "e"), inst("idle", "f")],
    })
    render(<CapacitySegments status={many} />)
    const bar = screen.getByRole("img", { name: "3 of 99 busy, 1 starting, 2 warm, 93 free" })
    const segments = [...bar.querySelectorAll<HTMLElement>("[data-kind]")]
    expect(segments.map((s) => s.getAttribute("data-kind"))).toEqual(["busy", "starting", "warm", "free"])
    expect(segments.map((s) => s.style.flexGrow)).toEqual(["3", "1", "2", "93"])
    for (const s of segments) expect(s).toHaveClass("min-w-0.5")
  })

  it("starts the over-max marker on a new group", () => {
    const flood = status({ global_max: 1, instances: Array.from({ length: 20 }, (_, i) => inst("busy", `r${i}`)) })
    const { container } = render(<CapacitySegments status={flood} />)
    const segments = [...container.querySelectorAll<HTMLElement>("[data-kind]")]
    expect(segments.map((s) => s.style.flexGrow)).toEqual(["1", "19"])
    expect(segments[1]).toHaveClass("ml-[2px]")
    expect(segments[0]).not.toHaveClass("ml-[2px]")
  })

  it.each([
    [16, 16],
    [17, 3],
  ])("with %i slots draws %i segments", (max, drawn) => {
    const edge = status({ global_max: max, instances: [inst("busy", "a"), inst("busy", "b"), inst("idle", "c")] })
    const { container } = render(<CapacitySegments status={edge} />)
    expect(container.querySelectorAll("[data-kind]")).toHaveLength(drawn)
  })
})
