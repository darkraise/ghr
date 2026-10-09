import { describe, expect, it } from "vitest"
import type { HistoryEntry } from "@/api/types"
import {
  dayLabel,
  groupByDay,
  groupByDayOf,
  historySummary,
  inBucket,
  median,
  parseHistorySearch,
  pickText,
  resultWord,
} from "./history"

const now = Date.parse("2026-10-03T14:05:00Z")
const entry = (id: string, start: string, end: string, conclusion = "success"): HistoryEntry => ({
  id,
  repo: "darkmem",
  run_id: 1,
  run_number: "1",
  workflow: "ci",
  job_name: "build",
  conclusion,
  started_at: start,
  finished_at: end,
})

describe("parseHistorySearch", () => {
  it("keeps the values it knows and drops the rest", () => {
    expect(parseHistorySearch({ repo: "darkmem", result: "failure", window: "30d" })).toEqual({ repo: "darkmem", result: "failure", window: "30d" })
    expect(parseHistorySearch({ repo: "", result: "bogus", window: "2h" })).toEqual({})
    expect(parseHistorySearch({ repo: 4 })).toEqual({})
  })
})

describe("dayLabel and groupByDay", () => {
  it("names today, yesterday, then the weekday and date", () => {
    expect(dayLabel("2026-10-03T00:10:00Z", now)).toBe("Today")
    expect(dayLabel("2026-10-02T23:50:00Z", now)).toBe("Yesterday")
    expect(dayLabel("2026-09-28T10:00:00Z", now)).toBe("Mon, Sep 28")
  })

  it("groups rows that are already newest first, keeping their order", () => {
    const rows = [
      entry("a", "2026-10-03T13:00:00Z", "2026-10-03T13:40:00Z"),
      entry("b", "2026-10-03T09:00:00Z", "2026-10-03T09:05:00Z"),
      entry("c", "2026-10-02T09:00:00Z", "2026-10-02T09:05:00Z"),
    ]
    const groups = groupByDay(rows, now)
    expect(groups.map((g) => g.label)).toEqual(["Today", "Yesterday"])
    expect(groups[0]?.rows.map((r) => r.id)).toEqual(["a", "b"])
  })
})

describe("groupByDayOf", () => {
  it("groups any rows by the day its accessor reads", () => {
    const now = Date.parse("2026-10-03T14:05:00Z")
    const rows = [
      { id: 1, at: "2026-10-03T13:00:00Z" },
      { id: 2, at: "2026-10-03T09:00:00Z" },
      { id: 3, at: "2026-10-02T23:00:00Z" },
    ]
    const groups = groupByDayOf(rows, (r) => r.at, now)
    expect(groups.map((g) => [g.label, g.rows.map((r) => r.id)])).toEqual([
      ["Today", [1, 2]],
      ["Yesterday", [3]],
    ])
  })
})

describe("median and historySummary", () => {
  it("takes the middle value, or the mean of the two middle ones", () => {
    expect(median([1, 3, 2])).toBe(2)
    expect(median([4, 1, 3, 2])).toBe(2.5)
    expect(median([])).toBeUndefined()
  })

  it("counts jobs and failures and gives the median duration", () => {
    const rows = [
      entry("a", "2026-10-03T13:35:00Z", "2026-10-03T13:40:00Z"),
      entry("b", "2026-10-03T11:05:00Z", "2026-10-03T11:06:30Z", "failure"),
    ]
    expect(historySummary(rows)).toBe("2 jobs, 1 failed, median 3m15s")
    expect(historySummary([rows[0] as HistoryEntry])).toBe("1 job, median 5m00s")
    expect(historySummary([])).toBe("No jobs")
  })
})

describe("inBucket and pickText", () => {
  it("keeps rows that finished inside the bucket", () => {
    const rows = [
      entry("in", "2026-10-03T13:00:00Z", "2026-10-03T13:40:00Z"),
      entry("edge", "2026-10-03T13:50:00Z", "2026-10-03T14:00:00Z"),
      entry("out", "2026-10-03T11:00:00Z", "2026-10-03T11:06:30Z"),
    ]
    expect(inBucket(rows, { start: "2026-10-03T13:00:00Z", end: "2026-10-03T14:00:00Z" }).map((r) => r.id)).toEqual(["in"])
  })

  it("describes the picked span", () => {
    expect(pickText({ start: "2026-10-03T13:00:00Z", end: "2026-10-03T14:00:00Z" })).toBe("Showing 13:00 to 14:00, Oct 3")
    expect(pickText({ start: "2026-10-03T00:00:00Z", end: "2026-10-04T00:00:00Z" })).toBe("Showing Oct 3")
  })
})

describe("resultWord", () => {
  it.each([
    ["success", "Succeeded"],
    ["cancelled", "Cancelled"],
    ["skipped", "Skipped"],
    ["unknown", "Unknown"],
    ["failure", "Failed"],
    ["timed_out", "Failed"],
  ])("reads %s as %s", (conclusion, word) => {
    expect(resultWord(conclusion)).toBe(word)
  })
})
