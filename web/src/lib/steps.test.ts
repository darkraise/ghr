import { describe, expect, it } from "vitest"
import type { Step } from "@/api/types"
import { stepPhase, stepTimeline } from "./steps"

const t = (hms: string) => `2026-10-03T${hms}Z`
const steps: Step[] = [
  { number: 1, name: "checkout", status: "completed", conclusion: "success", started_at: t("14:00:00"), completed_at: t("14:01:00") },
  { number: 2, name: "test", status: "in_progress", conclusion: "", started_at: t("14:01:00") },
  { number: 3, name: "upload", status: "queued", conclusion: "" },
]

describe("stepPhase", () => {
  it.each([
    ["in_progress", "running"],
    ["completed", "completed"],
    ["queued", "pending"],
    ["waiting", "pending"],
    ["pending", "pending"],
  ])("reads %s as %s", (status, phase) => {
    expect(stepPhase({ number: 1, name: "x", status, conclusion: "" })).toBe(phase)
  })
})

describe("stepTimeline", () => {
  it("runs the axis to now while the job runs", () => {
    const tl = stepTimeline(steps, Date.parse(t("14:03:00")), true)
    expect(tl.bars).toEqual([{ offset: 0, width: 1 / 3 }, { offset: 1 / 3, width: 2 / 3 }, null])
    expect(tl.durations).toEqual([60_000, 120_000, null])
  })

  it("ends the axis at the last step's end once the runner has finished", () => {
    const done: Step[] = [
      steps[0] as Step,
      { ...(steps[1] as Step), status: "completed", conclusion: "failure", completed_at: t("14:02:00") },
      steps[2] as Step,
    ]
    const tl = stepTimeline(done, Date.parse(t("14:30:00")), false)
    expect(tl.bars).toEqual([{ offset: 0, width: 0.5 }, { offset: 0.5, width: 0.5 }, null])
    expect(tl.durations).toEqual([60_000, 60_000, null])
  })

  it("draws nothing when no step reports a time", () => {
    const tl = stepTimeline([{ number: 1, name: "x", status: "queued", conclusion: "" }], Date.parse(t("14:00:00")), true)
    expect(tl.bars).toEqual([null])
    expect(tl.durations).toEqual([null])
  })
})
