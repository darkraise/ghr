import { render, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import type { RunDetail } from "@/api/types"
import { RunStatusIcon } from "@/components/run-status-icon"
import { fixtures } from "@/test/fixtures"
import {
  annotationCounts,
  attemptLabel,
  firstFailedStep,
  githubLinks,
  isRunActive,
  jobMs,
  parseJobSearch,
  parseRunSearch,
  positiveInt,
  rawLogFileName,
  runDescription,
  runTotalMs,
  statusText,
} from "./run-details"

const run = fixtures.runDetail
const now = Date.parse("2026-10-03T14:05:00Z")

describe("route parameters", () => {
  it.each([
    [1, 1],
    ["42", 42],
    ["01", 1],
    [0, undefined],
    [-3, undefined],
    [1.5, undefined],
    ["1.5", undefined],
    ["abc", undefined],
    ["", undefined],
    ["99999999999999999999", undefined],
    [undefined, undefined],
    [null, undefined],
  ])("positiveInt(%j) is %j", (v, want) => {
    expect(positiveInt(v)).toBe(want)
  })

  it("keeps only positive integers in the search", () => {
    expect(parseRunSearch({ attempt: "2", step: 3 })).toEqual({ attempt: 2 })
    expect(parseRunSearch({ attempt: "x" })).toEqual({})
    expect(parseJobSearch({ attempt: 1, step: "3", line: 0, other: 5 })).toEqual({ attempt: 1, step: 3 })
  })
})

describe("run facts", () => {
  it("names the status of a run or job", () => {
    expect(isRunActive("completed")).toBe(false)
    expect(isRunActive("queued")).toBe(true)
    expect(statusText("completed", "failure")).toBe("Failed")
    expect(statusText("completed", "success")).toBe("Succeeded")
    expect(statusText("in_progress", "")).toBe("Running")
    expect(statusText("waiting", "")).toBe("Queued")
  })

  it("times a job from its start to its completion, or to now", () => {
    const backend = run.jobs[1]
    if (!backend) throw new Error("no job")
    expect(jobMs(backend, now)).toBe(135_000)
    expect(jobMs({ ...backend, completed_at: undefined }, Date.parse("2026-10-03T13:56:25Z"))).toBe(60_000)
    expect(jobMs({ ...backend, started_at: undefined, completed_at: undefined }, now)).toBeUndefined()
  })

  it("times the run to its last job's completion, or to now while active", () => {
    expect(runTotalMs(run, now)).toBe(250_000)
    const active: RunDetail = { ...run, status: "in_progress" }
    expect(runTotalMs(active, Date.parse("2026-10-03T13:57:00Z"))).toBe(120_000)
  })

  it("describes the run in one sentence", () => {
    expect(runDescription(run)).toBe("ci #42 in darkmem on fix/cache-key.")
    expect(runDescription({ ...run, branch: "" })).toBe("ci #42 in darkmem.")
  })

  it("builds GitHub links from the owner", () => {
    const links = githubLinks("darkraise", run)
    expect(links.commit).toBe("https://github.com/darkraise/darkmem/commit/a1f9c3e0d4b5c6a7b8c9d0e1f2a3b4c5d6e7f8a9")
    expect(links.workflowFile).toBe(
      "https://github.com/darkraise/darkmem/blob/a1f9c3e0d4b5c6a7b8c9d0e1f2a3b4c5d6e7f8a9/.github/workflows/ci.yml",
    )
    expect(links.pull(87)).toBe("https://github.com/darkraise/darkmem/pull/87")
    expect(githubLinks("darkraise", { ...run, path: "dynamic/dependabot/dependabot-updates" }).workflowFile).toBeUndefined()
  })

  it("encodes each part of the workflow file's path", () => {
    expect(githubLinks("darkraise", { ...run, path: ".github/workflows/ci#prod.yml" }).workflowFile).toBe(
      "https://github.com/darkraise/darkmem/blob/a1f9c3e0d4b5c6a7b8c9d0e1f2a3b4c5d6e7f8a9/.github/workflows/ci%23prod.yml",
    )
  })

  it("links the workflow file of a path that carries the ref it ran from", () => {
    expect(githubLinks("darkraise", { ...run, path: ".github/workflows/ci.yml@refs/heads/main" }).workflowFile).toBe(
      "https://github.com/darkraise/darkmem/blob/a1f9c3e0d4b5c6a7b8c9d0e1f2a3b4c5d6e7f8a9/.github/workflows/ci.yml",
    )
  })

  it("labels attempts", () => {
    expect(attemptLabel(2, 2)).toBe("Attempt 2 (latest)")
    expect(attemptLabel(1, 2)).toBe("Attempt 1")
  })

  it("counts annotations by level", () => {
    expect(annotationCounts(fixtures.runAnnotations.items)).toEqual({ error: 2, warning: 1, notice: 1 })
    expect(annotationCounts([])).toEqual({ error: 0, warning: 0, notice: 0 })
  })

  it("finds the first failed step", () => {
    expect(firstFailedStep(run.jobs[1]?.steps ?? [])).toBe(3)
    expect(firstFailedStep(run.jobs[0]?.steps ?? [])).toBeUndefined()
  })

  it("names the raw log file", () => {
    expect(rawLogFileName("darkmem", 1002)).toBe("darkmem-job-1002.log")
  })
})

describe("RunStatusIcon", () => {
  it.each([
    ["completed", "success", "Succeeded"],
    ["completed", "failure", "Failed"],
    ["completed", "cancelled", "Cancelled"],
    ["completed", "skipped", "Skipped"],
    ["queued", "", "Waiting"],
    ["waiting", "", "Waiting"],
  ])("shows %s/%s as %s", (status, conclusion, name) => {
    render(<RunStatusIcon status={status} conclusion={conclusion} />)
    expect(screen.getByRole("img", { name })).toBeInTheDocument()
  })

  it("shows a running job as a spinner", () => {
    render(<RunStatusIcon status="in_progress" conclusion="" />)
    expect(screen.getByText("Running")).toHaveClass("sr-only")
    expect(screen.queryByRole("img", { name: "Waiting" })).toBeNull()
  })
})
