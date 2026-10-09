import { describe, expect, it } from "vitest"
import type { ActionsRun } from "@/api/types"
import { fixtures } from "@/test/fixtures"
import {
  actionsSummary,
  isActive,
  isQueued,
  parseActionsSearch,
  RECENT_LIMIT,
  repoErrorText,
  runMs,
  splitRuns,
  startText,
  statusWord,
} from "./actions"

const now = Date.parse("2026-10-03T14:05:00Z")
const runs = fixtures.actions.runs
const byId = (id: number): ActionsRun => {
  const r = runs.find((x) => x.id === id)
  if (!r) throw new Error(`no run ${id}`)
  return r
}
const run = (over: Partial<ActionsRun>): ActionsRun => ({ ...byId(101), ...over })

describe("parseActionsSearch", () => {
  it("keeps known values and drops the rest", () => {
    expect(parseActionsSearch({ repo: "darkmem", status: "failed" })).toEqual({ repo: "darkmem", status: "failed" })
    expect(parseActionsSearch({ repo: "", status: "bogus", other: 1 })).toEqual({})
  })
})

describe("run states", () => {
  it.each([
    ["queued", true, true, "Queued"],
    ["waiting", true, true, "Queued"],
    ["pending", true, true, "Queued"],
    ["requested", true, true, "Queued"],
    ["action_required", true, true, "Queued"],
    ["in_progress", true, false, "Running"],
  ])("reads %s", (status, active, queued, word) => {
    const r = run({ status, conclusion: "" })
    expect([isActive(r), isQueued(r), statusWord(r)]).toEqual([active, queued, word])
  })

  it("words a completed run by its result", () => {
    expect(statusWord(run({ conclusion: "success" }))).toBe("Succeeded")
    expect(statusWord(run({ conclusion: "timed_out" }))).toBe("Failed")
    expect(statusWord(run({ conclusion: "skipped" }))).toBe("Skipped")
  })
})

describe("splitRuns", () => {
  it("splits active from recent and filters by repository and status", () => {
    expect(splitRuns(runs, undefined, undefined)).toEqual({ active: [byId(300), byId(102)], recent: [byId(101), byId(90)] })
    expect(splitRuns(runs, "DarkMem", undefined)).toEqual({ active: [byId(102)], recent: [byId(101)] })
    expect(splitRuns(runs, undefined, "active")).toEqual({ active: [byId(300), byId(102)], recent: [] })
    expect(splitRuns(runs, undefined, "failed")).toEqual({ active: [], recent: [byId(90)] })
    expect(splitRuns(runs, undefined, "succeeded")).toEqual({ active: [], recent: [byId(101)] })
  })

  it("filters before it caps the recent runs", () => {
    const many = Array.from({ length: 150 }, (_, i) => run({ id: i, conclusion: i < 120 ? "success" : "failure" }))
    expect(splitRuns(many, undefined, undefined).recent).toHaveLength(RECENT_LIMIT)
    expect(splitRuns(many, undefined, "failed").recent).toHaveLength(30)
  })
})

describe("times", () => {
  it("measures a running run to now and a finished one to its update", () => {
    expect(runMs(byId(102), now)).toBe(4 * 60_000)
    expect(runMs(byId(101), now)).toBe(5 * 60_000)
  })

  it("shows the day before today", () => {
    expect(startText("2026-10-03T13:35:00Z", now)).toBe("13:35")
    expect(startText("2026-10-02T14:05:00Z", now)).toBe("Oct 2 14:05")
  })
})

describe("text", () => {
  it("sums up the runs across repositories or in one", () => {
    const at = new Date("2026-10-03T14:05:00Z")
    expect(actionsSummary(runs, 4, undefined, at)).toBe("1 running, 1 queued across 4 repositories. Updated 14:05.")
    expect(actionsSummary(runs, 1, undefined, at)).toBe("1 running, 1 queued across 1 repository. Updated 14:05.")
    expect(actionsSummary(runs, 4, "darkcloud", at)).toBe("0 running, 0 queued in darkcloud. Updated 14:05.")
    const waiting = [...runs, run({ id: 1, status: "action_required", conclusion: "" })]
    expect(actionsSummary(waiting, 4, undefined, at)).toBe("1 running, 2 queued across 4 repositories. Updated 14:05.")
  })

  it("words a repository's error like errorText", () => {
    const oldDocs = fixtures.actions.repos.find((r) => r.repo === "old-docs")
    if (!oldDocs) throw new Error("no old-docs repo")
    expect(repoErrorText(oldDocs)).toBe("old-docs: GitHub rate limit; API calls are paused. Retry after 14:30")
    expect(repoErrorText({ repo: "x", watched: false, error: "GitHub did not answer in time" })).toBe("x: GitHub did not answer in time")
  })
})
