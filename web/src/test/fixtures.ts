import actionsJson from "@/api/fixtures/actions.json"
import activityBucketsJson from "@/api/fixtures/activity-buckets.json"
import activityLanesJson from "@/api/fixtures/activity-lanes.json"
import artifactStoreJson from "@/api/fixtures/artifact-store.json"
import availableReposJson from "@/api/fixtures/available-repos.json"
import configJson from "@/api/fixtures/config.json"
import containersJson from "@/api/fixtures/containers.json"
import eventsJson from "@/api/fixtures/events.json"
import githubStorageJson from "@/api/fixtures/github-storage.json"
import historyJson from "@/api/fixtures/history.json"
import jobLogJson from "@/api/fixtures/job-log.json"
import labelCheckJson from "@/api/fixtures/label-check.json"
import logJson from "@/api/fixtures/log.json"
import metricsJson from "@/api/fixtures/metrics.json"
import packageSearchJson from "@/api/fixtures/package-search.json"
import packagesJson from "@/api/fixtures/packages.json"
import registrationsJson from "@/api/fixtures/registrations.json"
import runAnnotationsJson from "@/api/fixtures/run-annotations.json"
import runDetailJson from "@/api/fixtures/run-detail.json"
import statusJson from "@/api/fixtures/status.json"
import stepsJson from "@/api/fixtures/steps.json"
import storageJson from "@/api/fixtures/storage.json"
import storageAlertJson from "@/api/fixtures/storage-alert.json"
import tokenJson from "@/api/fixtures/token.json"
import choicesJson from "@/api/fixtures/toolchain-choices.json"
import type {
  Actions,
  Activity,
  ArtifactStore,
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
  PackageInfo,
  Packages,
  Registration,
  RunAnnotations,
  RunDetail,
  Status,
  Step,
  Storage,
  StorageAlert,
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
const activityLanes: Activity = activityLanesJson
const activityBuckets: Activity = activityBucketsJson
const actions: Actions = actionsJson
const runDetail: RunDetail = runDetailJson
const runAnnotations: RunAnnotations = runAnnotationsJson
const jobLog: JobLog = jobLogJson
const githubStorage: GitHubStorage = githubStorageJson
const storageAlert: StorageAlert = storageAlertJson
// The JSON import types state as a plain string.
const artifactStore = artifactStoreJson as ArtifactStore
const packages: Packages = packagesJson
const packageSearch: PackageInfo[] = packageSearchJson

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
  activityLanes,
  activityBuckets,
  actions,
  runDetail,
  runAnnotations,
  jobLog,
  githubStorage,
  storageAlert,
  artifactStore,
  packages,
  packageSearch,
}

export function authedRoutes(over: Record<string, unknown> = {}): Record<string, unknown> {
  return {
    "GET /auth/state": { setup_required: false, authenticated: true },
    "GET /api/status": fixtures.status,
    "GET /api/config": fixtures.config,
    "GET /api/metrics": fixtures.metrics,
    "GET /api/events": fixtures.events,
    "GET /api/activity": ({ url }: { url: URL }) =>
      ["1h", "3h"].includes(url.searchParams.get("window") ?? "") ? fixtures.activityLanes : fixtures.activityBuckets,
    "GET /api/storage": fixtures.storage,
    "GET /api/actions": fixtures.actions,
    "GET /api/github-storage": fixtures.githubStorage,
    "GET /api/artifact-store": fixtures.artifactStore,
    "GET /api/packages": fixtures.packages,
    ...over,
  }
}

// The run page's routes for run 102 of darkmem and its failed job 1002.
export function runRoutes(over: Record<string, unknown> = {}): Record<string, unknown> {
  return authedRoutes({
    "GET /api/actions/darkmem/runs/102": fixtures.runDetail,
    "GET /api/actions/darkmem/runs/102/annotations": fixtures.runAnnotations,
    "GET /api/actions/darkmem/jobs/1002/log": fixtures.jobLog,
    ...over,
  })
}
