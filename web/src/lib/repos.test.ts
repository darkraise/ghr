import { describe, expect, it } from "vitest"
import type { RepoStatus } from "@/api/types"
import { fixtures } from "@/test/fixtures"
import { repoDescription, reposSummary, runnersText, weekRate, weekText } from "./repos"

const repo = (over: Partial<RepoStatus>): RepoStatus => ({ name: "darkmem", paused: false, max: 3, active: 0, queued: 0, ...over })

describe("weekRate", () => {
  it.each([
    [{ succeeded: 46, failed: 2, cancelled: 1 }, 96],
    [{ succeeded: 1, failed: 1, cancelled: 0 }, 50],
    [{ succeeded: 0, failed: 0, cancelled: 4 }, undefined],
    [undefined, undefined],
  ])("reads %j as %s", (week, rate) => {
    expect(weekRate(week)).toBe(rate)
  })
})

describe("weekText", () => {
  it("names every count", () => {
    expect(weekText({ succeeded: 46, failed: 2, cancelled: 1 })).toBe("46 succeeded, 2 failed, 1 cancelled in 7 days")
  })
})

describe("reposSummary", () => {
  it("counts configured, running and paused repositories, leaving out those being removed", () => {
    expect(reposSummary(fixtures.status)).toBe("2 configured, 1 running, 1 paused")
  })
  it("leaves out a count of 0", () => {
    expect(reposSummary({ ...fixtures.status, repos: [repo({})] })).toBe("1 configured")
    expect(reposSummary({ ...fixtures.status, repos: [] })).toBe("")
  })
})

describe("runnersText", () => {
  it("shows the cap, or no limit", () => {
    expect(runnersText(repo({ active: 2, max: 3 }))).toBe("2 of 3")
    expect(runnersText(repo({ active: 1, max: 0 }))).toBe("1, no limit")
  })
})

describe("repoDescription", () => {
  it.each([
    [repo({ active: 2, queued: 1 }), "Running 2 of 3 runners, 1 job waiting"],
    [repo({ active: 1, max: 0 }), "Running 1 runner"],
    [repo({}), "Idle"],
    [repo({ queued: 2 }), "Idle, 2 jobs waiting"],
    [repo({ paused: true, queued: 2 }), "Paused"],
    [repo({ paused: true, error: "GitHub: not found" }), "GitHub: not found"],
  ])("describes %j", (r, text) => {
    expect(repoDescription(r)).toBe(text)
  })
})
