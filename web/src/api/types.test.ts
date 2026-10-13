import { describe, expect, it } from "vitest"
import { fixtures, runRoutes } from "@/test/fixtures"
import actionsJson from "./fixtures/actions.json"
import activityBucketsJson from "./fixtures/activity-buckets.json"
import activityLanesJson from "./fixtures/activity-lanes.json"
import availableReposJson from "./fixtures/available-repos.json"
import choicesJson from "./fixtures/toolchain-choices.json"
import configJson from "./fixtures/config.json"
import containersJson from "./fixtures/containers.json"
import eventsJson from "./fixtures/events.json"
import githubStorageJson from "./fixtures/github-storage.json"
import historyJson from "./fixtures/history.json"
import jobLogJson from "./fixtures/job-log.json"
import labelCheckJson from "./fixtures/label-check.json"
import logJson from "./fixtures/log.json"
import metricsJson from "./fixtures/metrics.json"
import registrationsJson from "./fixtures/registrations.json"
import runAnnotationsJson from "./fixtures/run-annotations.json"
import runDetailJson from "./fixtures/run-detail.json"
import statusDegradedJson from "./fixtures/status-degraded.json"
import statusJson from "./fixtures/status.json"
import storageAlertJson from "./fixtures/storage-alert.json"
import stepsJson from "./fixtures/steps.json"
import storageJson from "./fixtures/storage.json"
import tokenJson from "./fixtures/token.json"
import type {
  Actions,
  Activity,
  AvailableRepo,
  Config,
  Container,
  GhrEvent,
  GitHubStorage,
  HistoryEntry,
  JobLog,
  LabelCheck,
  LogChunk,
  Metrics,
  Registration,
  RunAnnotations,
  RunDetail,
  Status,
  Step,
  Storage,
  StorageAlert,
  TokenStatus,
  ToolchainChoice,
} from "./types"

// Assigning each fixture to its type makes `tsc --noEmit` fail when a Go
// field the UI declares is renamed or retyped.
const status: Status = statusJson
const degraded: Status = statusDegradedJson
const events: GhrEvent[] = eventsJson
const history: HistoryEntry[] = historyJson
const log: LogChunk = logJson
const steps: Step[] = stepsJson
const containers: Container[] = containersJson
const metrics: Metrics = metricsJson
const config: Config = configJson
const storage: Storage = storageJson
const token: TokenStatus = tokenJson
const labelCheck: LabelCheck = labelCheckJson
const registrations: Registration[] = registrationsJson
const availableRepos: AvailableRepo[] = availableReposJson
const choices: ToolchainChoice[] = choicesJson
const lanes: Activity = activityLanesJson
const buckets: Activity = activityBucketsJson

describe("type fixtures", () => {
  it("status carries the Dashboard fields", () => {
    expect(status.rate_limit).toBe(5000)
    expect(status.disk_used_bytes).toBe(146_000_000_000)
    expect(status.disk_total_bytes).toBe(240_000_000_000)
    expect(status.disk_root).toBe("/var/lib/docker")
    expect(degraded).not.toHaveProperty("disk_root")
    expect(status.repos[0]?.oldest_queued_at).toBe("2026-10-03T14:02:00Z")
    expect(degraded).not.toHaveProperty("rate_limit")
    expect(degraded.disk_used_bytes).toBe(0)
    expect(degraded.repos[0]).not.toHaveProperty("oldest_queued_at")
  })

  it("activity parses as lanes and as buckets", () => {
    expect(lanes.capacity).toBe(2)
    expect(lanes.lanes[0]?.runs[2]?.segments[0]?.to).toBeNull()
    expect(lanes.lanes[0]?.runs[0]).not.toHaveProperty("html_url")
    expect(lanes.cpu[2]?.cpu).toBeNull()
    expect(buckets.capacity).toBeNull()
    expect(buckets.buckets).toHaveLength(25)
    expect(buckets.buckets[0]?.busy_pct).toBeNull()
    expect(buckets.repos[0]?.hours).toHaveLength(24)
    expect(buckets.repos[0]?.week).toEqual({ succeeded: 1, failed: 1, cancelled: 0, unknown: 0 })
  })

  it("status carries the fields the UI reads", () => {
    expect(status.epoch).toBe("lz3k9a")
    expect(status.instances[0]?.job?.run_number).toBe("42")
    expect(status.instances[0]?.job?.html_url).toContain("https://")
    expect(status.repos[0]?.last_job?.finished_at).toBeTruthy()
    expect(status.runner_update.deadline).toBeTruthy()
    expect(status.maintenance.last_outcome).toBe("ok")
  })

  // tsc cannot catch a renamed optional field (an imported JSON value gets no
  // excess-property check, and a missing optional key is allowed), so this
  // asserts each one the UI reads is still present.
  it("the optional status fields keep their names", () => {
    expect(degraded.degraded).toBe(true)
    expect(degraded.degraded_reason).toBeTruthy()
    expect(degraded.repos[0]?.error).toBeTruthy()
    expect(degraded.repos[0]?.removing).toBe(true)
    expect(degraded.instances[0]).not.toHaveProperty("job")
    expect(degraded.maintenance.running).toBe(true)
    expect(degraded.runner_update.check_error).toBeTruthy()
    expect(degraded.runner_update.last_error).toBeTruthy()
    expect(degraded.runner_update.running).toBe(true)
    expect(degraded.runner_update.last_outcome).toBe("failed")
  })

  it("the live feeds carry their cursors", () => {
    expect(events.map((e) => e.seq)).toEqual([1, 2, 3])
    expect(log.next).not.toBe("")
    expect(history[0]?.conclusion).toBe("success")
    expect(steps[1]?.status).toBe("in_progress")
    expect(containers[0]?.project).toBe("ghr-aaaaaa")
    expect(metrics.samples).toHaveLength(3)
  })

  it("steps carry their times when GitHub reports them", () => {
    expect(steps[0]?.started_at).toBe("2026-10-03T14:01:00Z")
    expect(steps[0]?.completed_at).toBe("2026-10-03T14:01:06Z")
    expect(steps[1]).not.toHaveProperty("completed_at")
    expect(steps[2]).not.toHaveProperty("started_at")
  })

  it("config durations arrive as strings", () => {
    expect(config.poll_interval).toBe("10s")
    expect(config.history_retention).toBe("30d")
    expect(config.repos?.[0]?.max).toBe(2)
  })

  it("the later pages' types parse too", () => {
    expect(storage.operations.current?.progress).toBe("extracting")
    expect(storage.last_prune?.steps?.[0]?.freed).toBe(6800000)
    expect(token.rate_limit).toBe(5000)
    expect(labelCheck.groups[0]?.labels).toContain("homelab")
    expect(registrations[0]?.ghr).toBe(true)
    expect(availableRepos[1]?.configured).toBe(false)
    expect(choices[0]?.lts).toBe(true)
  })

  it("actions parse with runs, repos and the watch list", () => {
    const actions: Actions = actionsJson
    expect(actions.runs.map((r) => r.id)).toEqual([300, 102, 101, 90])
    expect(actions.runs.find((r) => r.id === 102)?.ghr).toBe(true)
    expect(actions.runs.find((r) => r.id === 300)?.watched).toBe(true)
    expect(actions.repos[3]).toEqual({
      repo: "old-docs",
      watched: true,
      error: "GitHub rate limit; API calls are paused",
      retry_at: "2026-10-03T14:30:00Z",
    })
    expect(actions.repos[0]).not.toHaveProperty("error")
  })

  it("config carries the watch list and available repos say what is watched", () => {
    expect(fixtures.config.watch_repos).toEqual(["docs", "old-docs"])
    expect(fixtures.availableRepos.map((r) => [r.name, r.watched])).toEqual([
      ["darkmem", false],
      ["docs", true],
      ["new-repo", false],
    ])
    expect(fixtures.actions.repos).toHaveLength(4)
  })
})

describe("GitHub storage fixtures", () => {
  const snap: GitHubStorage = githubStorageJson
  const alert: StorageAlert = storageAlertJson

  it("the snapshot carries the quota, the rows, the operation and the deletions", () => {
    expect(snap.quota?.level).toBe("warn")
    expect(snap.quota?.over_allowance_gb_hours).toBe(0)
    expect(snap.quota?.runs_out_at).toBe("2026-10-24T11:45:00Z")
    expect(snap.repos.map((r) => [r.name, r.covered, r.cleanup, r.cleanup_source])).toEqual([
      ["darkmem", true, "6h", "override"],
      ["darkcloud", true, "2d", "global"],
      ["sandbox", false, "2d", "global"],
    ])
    expect(snap.repos[0]?.artifacts[0]).toEqual({
      id: 9001,
      name: "frontend-dist",
      bytes: 157286400,
      created_at: "2026-10-02T14:05:00Z",
      expires_at: "2026-12-31T14:05:00Z",
      run_id: 102,
      branch: "master",
    })
    expect(snap.repos[0]?.caches[0]?.last_used_at).toBe("2026-10-03T13:05:00Z")
    expect(snap.repos[2]?.error).toContain("token lacks actions=read")
    expect(snap.repos[0]).not.toHaveProperty("error")
    expect(snap.operation?.failed[0]?.id).toBe(9003)
    expect(snap.deletions.map((d) => [d.kind, d.by])).toEqual([
      ["artifact", "cleanup"],
      ["cache", "manual"],
    ])
    expect(snap).not.toHaveProperty("quota_error")
  })

  it("the alert and the config block keep their names", () => {
    expect(alert).toEqual({
      level: "critical",
      month: "2026-10",
      used_gb_hours: 330.5,
      included_gb_hours: 363.28125,
      used_pct: 91,
      runs_out_at: "2026-10-06T02:05:00Z",
      resets_at: "2026-11-01T00:00:00Z",
    })
    expect(fixtures.config.github_storage).toEqual({ cleanup_max_age: "2d", repos: { darkmem: { cleanup: "6h" } } })
    expect(fixtures.status).not.toHaveProperty("github_storage")
  })
})

describe("run detail fixtures", () => {
  const run: RunDetail = runDetailJson
  const annotations: RunAnnotations = runAnnotationsJson
  const jobLog: JobLog = jobLogJson

  it("the run carries its attempts, jobs, graph and artifacts", () => {
    expect([run.id, run.run_number, run.attempt, run.attempts]).toEqual([102, 42, 2, 2])
    expect(run.pull_requests).toEqual([87])
    expect(run.jobs.map((j) => j.id)).toEqual([1001, 1002, 1003, 1004, 1005])
    expect(run.jobs[1]?.steps[3]).toEqual({ number: 4, name: "Test", status: "completed", conclusion: "skipped" })
    expect(run.graph?.nodes.map((n) => [n.key, n.column, n.group])).toEqual([
      ["pick-runner", 0, false],
      ["e2e", 0, true],
      ["backend", 1, false],
      ["frontend-tests", 1, true],
    ])
    expect(run.graph?.nodes[3]?.job_ids).toEqual([1003, 1004])
    expect(run).not.toHaveProperty("graph_note")
    expect(run.artifacts.map((a) => [a.name, a.expired])).toEqual([
      ["frontend-dist", false],
      ["coverage-backend", true],
    ])
  })

  it("annotations key the omitted counts by job ID", () => {
    expect(annotations.items.map((a) => a.level)).toEqual(["error", "error", "warning", "notice"])
    expect(annotations.omitted).toEqual({ "1002": 3 })
    expect([annotations.unread, annotations.expired_logs, annotations.too_large, annotations.failed]).toEqual([0, 0, 0, 0])
  })

  it("the job log keeps every kind and ANSI in the text", () => {
    expect(jobLog.steps.map((s) => s.lines.length)).toEqual([4, 2, 7, 0, 1])
    expect(jobLog.steps[2]?.lines[3]?.text).toBe("\u001b[31mFound 1 error.\u001b[0m")
    expect(jobLog.steps[2]?.lines.map((l) => l.kind)).toEqual(["group", "", "endgroup", "", "error", "debug", "error"])
  })

  it("runRoutes serves run 102 and lets a test replace one route", () => {
    expect(runRoutes()["GET /api/actions/darkmem/jobs/1002/log"]).toBe(fixtures.jobLog)
    expect(runRoutes()["GET /api/status"]).toBe(fixtures.status)
    expect(runRoutes({ "GET /api/actions/darkmem/runs/102": 1 })["GET /api/actions/darkmem/runs/102"]).toBe(1)
  })
})
