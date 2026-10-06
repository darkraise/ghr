import { describe, expect, it } from "vitest"
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
import statusJson from "./fixtures/status.json"
import stepsJson from "./fixtures/steps.json"
import storageJson from "./fixtures/storage.json"
import tokenJson from "./fixtures/token.json"
import type {
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

describe("type fixtures", () => {
  it("status carries the fields the UI reads", () => {
    expect(status.epoch).toBe("lz3k9a")
    expect(status.instances[0]?.job?.run_number).toBe("42")
    expect(status.instances[0]?.job?.html_url).toContain("https://")
    expect(status.repos[0]?.last_job?.finished_at).toBeTruthy()
    expect(status.runner_update.deadline).toBeTruthy()
    expect(status.maintenance.last_outcome).toBe("ok")
  })

  it("the live feeds carry their cursors", () => {
    expect(events.map((e) => e.seq)).toEqual([1, 2, 3])
    expect(log.next).not.toBe("")
    expect(history[0]?.conclusion).toBe("success")
    expect(steps[1]?.status).toBe("in_progress")
    expect(containers[0]?.project).toBe("ghr-aaaaaa")
    expect(metrics.samples).toHaveLength(3)
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
