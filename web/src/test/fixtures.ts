import availableReposJson from "@/api/fixtures/available-repos.json"
import configJson from "@/api/fixtures/config.json"
import containersJson from "@/api/fixtures/containers.json"
import eventsJson from "@/api/fixtures/events.json"
import historyJson from "@/api/fixtures/history.json"
import labelCheckJson from "@/api/fixtures/label-check.json"
import logJson from "@/api/fixtures/log.json"
import metricsJson from "@/api/fixtures/metrics.json"
import registrationsJson from "@/api/fixtures/registrations.json"
import statusJson from "@/api/fixtures/status.json"
import stepsJson from "@/api/fixtures/steps.json"
import storageJson from "@/api/fixtures/storage.json"
import tokenJson from "@/api/fixtures/token.json"
import choicesJson from "@/api/fixtures/toolchain-choices.json"
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
} from "@/api/types"

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
const toolchainChoices: ToolchainChoice[] = choicesJson

export const fixtures = {
  status,
  events,
  history,
  log,
  steps,
  containers,
  metrics,
  config,
  storage,
  token,
  labelCheck,
  registrations,
  availableRepos,
  toolchainChoices,
}

export function authedRoutes(over: Record<string, unknown> = {}): Record<string, unknown> {
  return {
    "GET /auth/state": { setup_required: false, authenticated: true },
    "GET /api/status": fixtures.status,
    "GET /api/config": fixtures.config,
    "GET /api/metrics": fixtures.metrics,
    "GET /api/events": fixtures.events,
    ...over,
  }
}
