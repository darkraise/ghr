export interface AuthState {
  setup_required: boolean
  authenticated: boolean
}

export interface JobInfo {
  run_id: number
  run_number: string
  workflow: string
  name: string
  html_url?: string
  started_at: string
}

export interface InstanceStatus {
  id: string
  repo: string
  runner_name: string
  state: string
  since: string
  job?: JobInfo
}

export interface HistoryEntry {
  id: string
  repo: string
  run_id: number
  run_number: string
  workflow: string
  job_name: string
  conclusion: string
  started_at: string
  finished_at: string
  html_url?: string
}

export interface RepoStatus {
  name: string
  paused: boolean
  removing?: boolean
  max: number
  active: number
  queued: number
  oldest_queued_at?: string
  error?: string
  last_job?: HistoryEntry
}

export interface MaintenanceStatus {
  running: boolean
  last_started?: string
  last_finished?: string
  last_outcome?: string
}

export interface RunnerUpdate {
  installed?: string
  latest?: string
  latest_published?: string
  deadline?: string
  checked_at?: string
  check_error?: string
  queued?: boolean
  queued_at?: string
  running?: boolean
  last_outcome?: string
  last_error?: string
  last_finished?: string
}

export interface Status {
  now: string
  epoch: string
  mode: string
  global_max: number
  degraded: boolean
  degraded_reason?: string
  rate_remaining: number
  rate_limit?: number
  disk_pct: number
  disk_used_bytes: number
  disk_total_bytes: number
  disk_root?: string
  repos: RepoStatus[]
  instances: InstanceStatus[]
  maintenance: MaintenanceStatus
  runner_update: RunnerUpdate
  web_setup_required?: boolean
  unconfigured?: boolean
  setup_pending?: boolean
}

export interface SetupState {
  configured: boolean
  starting: boolean
  setup_pending: boolean
  toolchains_pending: boolean
  owner: string
  web_listen: string
}

export interface GhrEvent {
  seq: number
  time: string
  level: string
  repo?: string
  msg: string
}

export interface Step {
  number: number
  name: string
  status: string
  conclusion: string
  started_at?: string
  completed_at?: string
}

export interface LogChunk {
  data: string
  next: string
}

export interface Container {
  id: string
  name: string
  image: string
  state: string
  project: string
}

export interface MetricSample {
  at: string
  live: number
  queued: number
  cpu?: number
  mem?: number
}

export interface Metrics {
  samples: MetricSample[]
  cpu?: number
  mem_used?: number
  mem_total?: number
  disk_pct: number
}

export interface RunnerLimits {
  memory_max: string
  cpu_quota: string
}

export interface WebConfig {
  listen?: string
  hosts?: string[]
}

export interface RepoConfig {
  name: string
  max?: number
  warm?: number
  labels?: string[]
  cleanup_name_prefixes?: string[]
  paused?: boolean
  removing?: boolean
}

export interface Config {
  owner: string
  mode: string
  global_max: number
  poll_interval: string
  start_timeout: string
  idle_timeout: string
  disk_high_water: number
  build_cache_keep: string
  history_retention: string
  labels: string[] | null
  runner_limits: RunnerLimits
  repos: RepoConfig[] | null
  watch_repos?: string[]
  web?: WebConfig
}

export interface TokenStatus {
  state: string
  checked_at?: string
  reason?: string
  rate_remaining?: number
  rate_limit?: number
  rate_reset?: string
  expires_at?: string
}

export interface Registration {
  id: number
  name: string
  status: string
  busy: boolean
  labels: string[]
  ghr: boolean
}

export interface LabelGroup {
  labels: string[]
  jobs: string[]
  more: number
  count: number
  last_seen: string
}

export interface LabelCheck {
  state: string
  checked_at?: string
  partial: boolean
  error?: string
  groups: LabelGroup[]
}

export interface AvailableRepo {
  name: string
  private: boolean
  configured: boolean
  watched: boolean
}

export interface Toolchain {
  tool: string
  version: string
  arch: string
  path: string
  bytes: number
  installed_at: string
}

export interface Folder {
  name: string
  bytes: number
}

export interface PackageCache {
  name: string
  label: string
  paths: string[] | null
  present: boolean
  bytes: number
  files: number
  last_written?: string
}

export interface DockerRow {
  type: string
  count: number
  active: number
  bytes: number
  reclaimable: number
}

export interface BuildCacheType {
  type: string
  count: number
  bytes: number
  reclaimable: number
}

export interface DockerDisk {
  rows: DockerRow[] | null
  build_cache_types: BuildCacheType[] | null
  disk_pct: number
}

export interface Operation {
  id: string
  kind: string
  target: string
  started_at: string
  progress?: string
  finished_at?: string
  outcome?: string
  message?: string
}

export interface Operations {
  current: Operation | null
  queued: number
  recent: Operation[] | null
}

export interface PruneStep {
  name: string
  freed: number
  error?: string
}

export interface LastPrune {
  trigger: string
  scope: string
  started_at: string
  finished_at?: string
  outcome?: string
  steps: PruneStep[] | null
}

export interface Storage {
  toolchains: Toolchain[] | null
  other_tool_cache: Folder[] | null
  package_caches: PackageCache[] | null
  docker: DockerDisk
  measured_at?: string
  measuring: boolean
  measure_error?: string
  operations: Operations
  last_prune: LastPrune | null
}

export interface ToolchainChoice {
  spec: string
  version: string
  lts?: boolean
}

export interface RunnerLimitsPatch {
  memory_max?: string
  cpu_quota?: string
}

export interface RepoPatch {
  max?: number
  warm?: number
  labels?: string[]
  cleanup_name_prefixes?: string[]
  paused?: boolean
}

export interface ConfigPatch {
  mode?: string
  global_max?: number
  poll_interval?: string
  start_timeout?: string
  idle_timeout?: string
  history_retention?: string
  disk_high_water?: number
  build_cache_keep?: string
  labels?: string[]
  runner_limits?: RunnerLimitsPatch
  repos?: Record<string, RepoPatch>
}

export interface AddRepoRequest {
  name: string
  max?: number
  labels?: string[]
  allow_public: boolean
}

export type PruneScope = "standard" | "build-cache-keep" | "build-cache-all" | "dangling-images" | "unused-volumes"

export type ActivityWindow = "1h" | "3h" | "24h" | "7d" | "30d"

export interface ActivitySegment {
  state: string
  from: string
  to: string | null
}

export interface ActivityRun {
  instance_id: string
  repo: string
  workflow?: string
  job?: string
  run_number?: string
  segments: ActivitySegment[]
  html_url?: string
}

export interface ActivityLane {
  runs: ActivityRun[]
}

export interface ActivityBucket {
  start: string
  end: string
  busy_minutes: number
  busy_pct: number | null
  succeeded: number
  failed: number
  cancelled: number
  unknown: number
  waiting_max: number | null
  cpu_avg: number | null
}

export interface ActivityPoint {
  at: string
  value: number
}

export interface ActivityCPU {
  at: string
  cpu: number | null
  mem: number | null
}

export interface ActivityHour {
  start: string
  succeeded: number
  failed: number
  cancelled: number
  unknown: number
}

export interface ActivityRepo {
  repo: string
  hours: ActivityHour[]
  week: ActivityWeek
}

export interface ActivityWeek {
  succeeded: number
  failed: number
  cancelled: number
  unknown: number
}

export interface Activity {
  window: string
  tz: string
  from: string
  to: string
  capacity: number | null
  history_from: string
  lanes: ActivityLane[]
  buckets: ActivityBucket[]
  waiting: ActivityPoint[]
  cpu: ActivityCPU[]
  repos: ActivityRepo[]
}

export interface ActionsRun {
  repo: string
  id: number
  run_number: number
  workflow: string
  title: string
  branch: string
  event: string
  actor: string
  status: string
  conclusion: string
  started_at: string
  updated_at: string
  html_url: string
  ghr: boolean
  watched: boolean
}

export interface ActionsRepo {
  repo: string
  watched: boolean
  error?: string
  retry_at?: string
}

export interface Actions {
  fetched_at: string
  runs: ActionsRun[]
  repos: ActionsRepo[]
}
