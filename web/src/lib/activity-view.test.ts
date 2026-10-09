import { describe, expect, it } from "vitest"
import type { ActivityBucket } from "@/api/types"
import { fixtures } from "@/test/fixtures"
import {
  activitySummary,
  bucketAria,
  bucketTickLabel,
  isLaneWindow,
  laneTicks,
  lineRuns,
  rangeText,
  retentionText,
  runAria,
  runEnd,
  runLabel,
  scaleX,
  sinceText,
  windowWords,
} from "./activity-view"

const lanes = fixtures.activityLanes
const buckets = fixtures.activityBuckets
const now = Date.parse("2026-10-03T14:05:00Z")
const HOUR = 3_600_000

describe("activity view", () => {
  it("tells lane windows from bucket windows and words them", () => {
    expect(["1h", "3h", "24h", "7d", "30d"].map(isLaneWindow)).toEqual([true, true, false, false, false])
    expect(windowWords("1h")).toBe("the last hour")
    expect(windowWords("3h")).toBe("the last 3 hours")
    expect(windowWords("30d")).toBe("the last 30 days")
  })

  it.each([
    ["1h", "2026-10-03T13:05:00Z", "2026-10-03T14:05:00Z", "13:05 to 14:05"],
    ["24h", "2026-10-02T14:00:00Z", "2026-10-03T14:05:00Z", "Oct 2 14:00 to Oct 3 14:05, per hour"],
    ["7d", "2026-09-27T00:00:00Z", "2026-10-03T14:05:00Z", "Sep 27 to Oct 3, per 6 hours"],
    ["30d", "2026-09-04T00:00:00Z", "2026-10-03T14:05:00Z", "Sep 4 to Oct 3, per day"],
  ])("states the %s range", (window, from, to, text) => {
    expect(rangeText(window, from, to)).toBe(text)
  })

  it("scales time to a width and clamps it", () => {
    expect(scaleX(50, 0, 100, 200)).toBe(100)
    expect(scaleX(-5, 0, 100, 200)).toBe(0)
    expect(scaleX(150, 0, 100, 200)).toBe(200)
  })

  it("ticks every 10 minutes in 1h and every 30 in 3h", () => {
    expect(laneTicks(now - HOUR, now, "1h").map((t) => t.label)).toEqual(["13:10", "13:20", "13:30", "13:40", "13:50", "14:00"])
    expect(laneTicks(now - 3 * HOUR, now, "3h").map((t) => t.label)).toEqual(["11:30", "12:00", "12:30", "13:00", "13:30", "14:00"])
  })

  it.each([
    ["24h", "2026-10-02T18:00:00Z", "18:00"],
    ["24h", "2026-10-02T19:00:00Z", null],
    ["7d", "2026-09-28T00:00:00Z", "Sep 28"],
    ["7d", "2026-09-28T06:00:00Z", null],
    ["30d", "2026-09-28T00:00:00Z", "Sep 28"],
    ["30d", "2026-09-29T00:00:00Z", null],
  ])("labels a %s tick at %s", (window, start, label) => {
    expect(bucketTickLabel(window, start)).toBe(label)
  })

  it("describes each run", () => {
    const runs = lanes.lanes.flatMap((l) => l.runs)
    expect(runs.map((r) => runAria(r, now))).toEqual([
      "darkmem, ci, lint, run 40, failed, 6m00s",
      "darkmem, ci, build, run 41, succeeded, 5m00s",
      "darkmem, ci, test, run 42, running, 4m00s",
      "darkmem, runner bbbbbb, warm, waiting for a job, 2m00s",
    ])
    expect(runs.map(runLabel)).toEqual(["darkmem lint #40", "darkmem build #41", "darkmem test #42", ""])
    expect(runs.map((r) => runEnd(r, now))).toEqual([Date.parse("2026-10-03T13:21:00Z"), Date.parse("2026-10-03T13:40:00Z"), now, now])
  })

  it("describes each bucket", () => {
    const lastClosed = buckets.buckets[23] as ActivityBucket
    expect(bucketAria(lastClosed, buckets)).toBe(
      "13:00 to 14:00, 5 busy runner-minutes, 1 succeeded, 0 failed, 0 cancelled, at most 3 waiting, CPU 23%",
    )
    expect(bucketAria({ ...lastClosed, busy_pct: 8.3 }, { window: "24h", capacity: 2 })).toContain("8.3% busy against today's max of 2")
    expect(bucketAria({ ...lastClosed, unknown: 2 }, buckets)).toContain("0 cancelled, 2 unknown, at most")
    const empty = bucketAria(buckets.buckets[0] as ActivityBucket, buckets)
    expect(empty).toContain("waiting not measured")
    expect(empty).toContain("CPU not measured")
  })

  it("sums up a window", () => {
    expect(activitySummary(lanes)).toBe("3 jobs in the last hour, 1 failed, 1 running.")
    expect(activitySummary(buckets)).toBe("2 jobs in the last 24 hours, 1 failed.")
    expect(activitySummary({ ...lanes, lanes: [] })).toBe("No jobs ran in the last hour.")
    const unknownOnly = buckets.buckets.map((b) => ({ ...b, succeeded: 0, failed: 0, cancelled: 0, unknown: 0 }))
    unknownOnly[3] = { ...(unknownOnly[3] as ActivityBucket), unknown: 1 }
    expect(activitySummary({ ...buckets, buckets: unknownOnly })).toBe("1 job in the last 24 hours, 0 failed.")
  })

  it("states retention and when metrics start", () => {
    expect(retentionText(lanes)).toBe("History is kept for 30 days")
    expect(retentionText({ ...lanes, history_from: "2026-10-03T00:05:00Z" })).toBe("History is kept for 14 hours")
    expect(sinceText("24h", "2026-10-03T13:00:00Z")).toBe("Collecting data since 13:00")
    expect(sinceText("7d", "2026-10-03T12:00:00Z")).toBe("Collecting data since Oct 3 12:00")
  })

  it("breaks lines at missing values", () => {
    const points = [
      { x: 0, v: 1 },
      { x: 1, v: null },
      { x: 2, v: 2 },
      { x: 3, v: 3 },
    ]
    expect(lineRuns(points, (v) => v * 10)).toEqual([[{ x: 0, y: 10 }], [{ x: 2, y: 20 }, { x: 3, y: 30 }]])
  })
})
