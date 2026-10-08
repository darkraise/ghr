import { describe, expect, it } from "vitest"
import type { InstanceStatus, RepoStatus, Status } from "@/api/types"
import { fixtures } from "@/test/fixtures"
import { dashboardSummary, runnersSummary } from "./summary"

const repo = (over: Partial<RepoStatus>): RepoStatus => ({ name: "darkmem", paused: false, max: 2, active: 0, queued: 0, ...over })
const inst = (state: string, id: string): InstanceStatus => ({ id, repo: "darkmem", runner_name: `ghr-${id}`, state, since: "2026-10-03T14:00:00Z" })
const st = (over: Partial<Status>): Status => ({ ...fixtures.status, instances: [], ...over })

describe("dashboardSummary", () => {
  it.each([
    ["the fixture", fixtures.status, "1 of 2 runners busy. 3 jobs are waiting in darkmem. old-repo: GitHub: not found."],
    ["nothing busy", st({ repos: [repo({})] }), "No runners busy."],
    ["all mode", st({ mode: "all", instances: [inst("busy", "a"), inst("busy", "b")], repos: [repo({})] }), "2 runners busy."],
    ["all paused", st({ repos: [repo({ paused: true }), repo({ name: "gone", removing: true })] }), "All repositories are paused."],
    [
      "paused while runners finish",
      st({ instances: [inst("busy", "a"), inst("busy", "b")], repos: [repo({ paused: true })] }),
      "All repositories are paused, and 2 runners are finishing.",
    ],
    ["one job waiting", st({ repos: [repo({ queued: 1 })] }), "No runners busy. 1 job is waiting in darkmem."],
    [
      "two repositories waiting",
      st({ repos: [repo({ queued: 2 }), repo({ name: "ghr", queued: 1 })] }),
      "No runners busy. 3 jobs are waiting in darkmem and ghr.",
    ],
    [
      "three repositories waiting",
      st({ repos: [repo({ queued: 1 }), repo({ name: "ghr", queued: 1 }), repo({ name: "darkcloud", queued: 1 })] }),
      "No runners busy. 3 jobs are waiting across 3 repositories.",
    ],
    [
      "two errors",
      st({ repos: [repo({ error: "GitHub: not found" }), repo({ name: "ghr", error: "GitHub: forbidden." })] }),
      "No runners busy. 2 repositories have errors.",
    ],
    ["an error ending in a period", st({ repos: [repo({ error: "GitHub: forbidden." })] }), "No runners busy. darkmem: GitHub: forbidden."],
    ["no repositories", st({ repos: [] }), "No runners busy."],
  ])("%s", (_name, status, sentence) => {
    expect(dashboardSummary(status)).toBe(sentence)
  })
})

describe("runnersSummary", () => {
  it("counts live runners and every waiting job", () => {
    expect(runnersSummary(fixtures.status)).toBe("2 live, 3 jobs waiting")
  })
  it("leaves cleaning runners out and says when none is live", () => {
    const instances = fixtures.status.instances.map((i) => ({ ...i, state: "cleaning" }))
    expect(runnersSummary({ ...fixtures.status, instances, repos: [] })).toBe("No live runners")
  })
  it("names a single waiting job", () => {
    const repos = fixtures.status.repos.map((r) => ({ ...r, queued: r.name === "darkmem" ? 1 : 0 }))
    expect(runnersSummary({ ...fixtures.status, repos })).toBe("2 live, 1 job waiting")
  })
})
