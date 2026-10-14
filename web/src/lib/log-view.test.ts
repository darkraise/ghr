import { describe, expect, it } from "vitest"
import type { JobLog, LogLine } from "@/api/types"
import { fixtures } from "@/test/fixtures"
import { containingGroup, findMatches, groupRows, LOG_WINDOW, logWindow } from "./log-view"

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

describe("logWindow", () => {
  it("renders every line of a step under the window", () => {
    expect(logWindow(many(LOG_WINDOW))).toEqual({ start: 0, end: LOG_WINDOW })
  })

  it("renders the last 5,000 lines of a longer step", () => {
    expect(logWindow(many(6000))).toEqual({ start: 1000, end: 6000 })
  })

  it("keeps the last lines for a line inside them or one that is not there", () => {
    expect(logWindow(many(6000), 5500)).toEqual({ start: 1000, end: 6000 })
    expect(logWindow(many(6000), 1001)).toEqual({ start: 1000, end: 6000 })
    expect(logWindow(many(6000), 99999)).toEqual({ start: 1000, end: 6000 })
  })

  it("centres a window on a line above them", () => {
    const { start, end } = logWindow(many(100_000), 50_000)
    expect(end - start).toBe(LOG_WINDOW)
    expect(start).toBe(49_999 - LOG_WINDOW / 2)
  })

  it("starts at the first line for a line near the top", () => {
    expect(logWindow(many(6000), 10)).toEqual({ start: 0, end: LOG_WINDOW })
    expect(logWindow(many(100_000), 1)).toEqual({ start: 0, end: LOG_WINDOW })
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
      no_log: false,
      steps: [{ number: 1, name: "s", lines: [line(1, "", "aa aa"), line(2, "endgroup", "aa")] }],
    }
    expect(findMatches(log, "aa")).toEqual([
      { step: 1, line: 1, index: 0 },
      { step: 1, line: 1, index: 1 },
    ])
    expect(findMatches(log, "")).toEqual([])
  })
})
