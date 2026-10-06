import { describe, expect, it } from "vitest"
import type { HistoryEntry } from "@/api/types"
import { activityWindow, summarize } from "./activity"

const NOW = Date.parse("2026-10-06T12:00:00Z")
const H = 3_600_000

function entry(id: string, conclusion: string, hoursAgo: number, minutes: number): HistoryEntry {
  const finished = NOW - hoursAgo * H
  return {
    id,
    repo: "darkmem",
    run_id: 1,
    run_number: id,
    workflow: "ci",
    job_name: "build",
    conclusion,
    started_at: new Date(finished - minutes * 60_000).toISOString(),
    finished_at: new Date(finished).toISOString(),
  }
}

// Newest first, as GET /history serves it.
const entries = [
  entry("5", "success", 1, 5),
  entry("4", "failure", 2, 3),
  entry("3", "cancelled", 3, 1),
  entry("2", "", 4, 1),
  entry("1", "success", 8 * 24, 9),
]

describe("activityWindow", () => {
  it("is 7 days unless the retention is shorter", () => {
    expect(activityWindow(undefined)).toEqual({ ms: 7 * 24 * H, label: "last 7 days" })
    expect(activityWindow("30d")).toEqual({ ms: 7 * 24 * H, label: "last 7 days" })
    expect(activityWindow("3d")).toEqual({ ms: 3 * 24 * H, label: "last 3d" })
  })
})

describe("summarize", () => {
  it("counts the window's jobs, the success rate over known results and the average", () => {
    const a = summarize(entries, NOW, "30d", 500)
    expect(a.line).toBe("4 jobs · 33% success · avg 2m30s")
    expect(a.label).toBe("last 7 days")
  })

  it("draws the newest results oldest first, with a name for each mark", () => {
    const a = summarize(entries, NOW, undefined, 500)
    expect(a.strip.map((m) => m.symbol)).toEqual(["?", "○", "✖", "■"])
    expect(a.strip.at(-1)?.label).toBe("#5 build: success")
    expect(a.strip[0]?.label).toBe("#2 build: unknown")
  })

  it("marks a capped read and an empty window", () => {
    expect(summarize(entries, NOW, undefined, 5).line).toBe("≥4 jobs · 33% success · avg 2m30s")
    expect(summarize([], NOW, undefined, 500)).toEqual({ line: "0 jobs · – success · avg –", label: "last 7 days", strip: [] })
  })
})
