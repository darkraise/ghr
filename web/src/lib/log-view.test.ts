import { describe, expect, it } from "vitest"
import type { JobLog, LogLine } from "@/api/types"
import { fixtures } from "@/test/fixtures"
import { containingGroup, findMatches, groupRows, LOG_WINDOW, windowStart } from "./log-view"

const line = (n: number, kind = "", text = `line ${n}`): LogLine => ({ n, kind, text })
const many = (count: number): LogLine[] => Array.from({ length: count }, (_, i) => line(i + 1))

describe("groupRows", () => {
  it("folds a group's lines under it and hides endgroup", () => {
    const lint = fixtures.jobLog.steps[2]?.lines ?? []
    const rows = groupRows(lint)
    expect(rows.map((r) => [r.type, r.line.n])).toEqual([
      ["group", 1],
      ["line", 4],
      ["line", 5],
      ["line", 6],
      ["line", 7],
    ])
    const group = rows[0]
    expect(group?.type === "group" ? group.lines.map((l) => l.n) : []).toEqual([2])
  })

  it("runs a group without endgroup to the end and lets a new group close the last", () => {
    const rows = groupRows([line(1, "group", "a"), line(2), line(3, "group", "b"), line(4), line(5)])
    expect(rows.map((r) => (r.type === "group" ? [r.line.n, r.lines.map((l) => l.n)] : [r.line.n]))).toEqual([
      [1, [2]],
      [3, [4, 5]],
    ])
  })
})

describe("containingGroup", () => {
  const lines = [line(1, "group", "a"), line(2), line(3, "endgroup", ""), line(4)]
  it.each([
    [2, 1],
    [1, undefined],
    [4, undefined],
    [9, undefined],
  ])("line %i is inside group %j", (n, want) => {
    expect(containingGroup(lines, n)).toBe(want)
  })
})

describe("windowStart", () => {
  it("renders every line of a step under the window", () => {
    expect(windowStart(many(LOG_WINDOW))).toBe(0)
  })

  it("renders the last 5,000 lines of a longer step", () => {
    expect(windowStart(many(6000))).toBe(1000)
    expect(windowStart(many(6000), 5500)).toBe(1000)
  })

  it("reaches back to a line above the cut", () => {
    expect(windowStart(many(6000), 10)).toBe(9)
    expect(windowStart(many(6000), 99999)).toBe(1000)
  })
})

describe("findMatches", () => {
  it("finds every occurrence, case-insensitively, without ANSI codes, in step order", () => {
    expect(findMatches(fixtures.jobLog, "FOUND")).toEqual([{ step: 3, line: 4, index: 0 }])
    expect(findMatches(fixtures.jobLog, "ruff").map((m) => [m.step, m.line, m.index])).toEqual([
      [2, 2, 0],
      [3, 1, 0],
      [3, 2, 0],
      [3, 6, 0],
    ])
  })

  it("counts several occurrences in one line and skips endgroup lines", () => {
    const log: JobLog = {
      job_id: 1,
      too_large: false,
      expired: false,
      steps: [{ number: 1, name: "s", lines: [line(1, "", "aa aa"), line(2, "endgroup", "aa")] }],
    }
    expect(findMatches(log, "aa")).toEqual([
      { step: 1, line: 1, index: 0 },
      { step: 1, line: 1, index: 1 },
    ])
    expect(findMatches(log, "")).toEqual([])
  })
})
