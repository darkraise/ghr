import { describe, expect, it } from "vitest"
import activityBucketsJson from "./fixtures/activity-buckets.json"
import activityLanesJson from "./fixtures/activity-lanes.json"
import availableReposJson from "./fixtures/available-repos.json"
import choicesJson from "./fixtures/toolchain-choices.json"
import configJson from "./fixtures/config.json"
import containersJson from "./fixtures/containers.json"
import eventsJson from "./fixtures/events.json"
import historyJson from "./fixtures/history.json"
import labelCheckJson from "./fixtures/label-check.json"
import logJson from "./fixtures/log.json"
import metricsJson from "./fixtures/metrics.json"
import registrationsJson from "./fixtures/registrations.json"
import statusDegradedJson from "./fixtures/status-degraded.json"
import statusJson from "./fixtures/status.json"
import stepsJson from "./fixtures/steps.json"
import storageJson from "./fixtures/storage.json"
import tokenJson from "./fixtures/token.json"
import type {
  Activity,
  AvailableRepo,
  Config,
  Container,
  GhrEvent,
  HistoryEntry,
  LabelCheck,
  LogChunk,
  Metrics,
  Registration,
  Status,
  Step,
  Storage,
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
    expect(buckets.repos[0]?.week).toEqual({ succeeded: 1, failed: 1, cancelled: 0 })
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
})
