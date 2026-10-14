import type {
  Actions,
  Activity,
  ActivityWindow,
  AddRepoRequest,
  AuthState,
  AvailableRepo,
  Config,
  ConfigPatch,
  Container,
  DownloadURL,
  GhrEvent,
  GitHubStorage,
  HistoryEntry,
  JobLog,
  LabelCheck,
  LogChunk,
  Metrics,
  PruneScope,
  Registration,
  RunAnnotations,
  RunDetail,
  SetupState,
  Status,
  Step,
  Storage,
  StoragePreview,
  TokenStatus,
  ToolchainChoice,
} from "./types"

export class ApiError extends Error {
  readonly status: number
  readonly retryAt?: Date

  constructor(status: number, message: string, retryAt?: Date) {
    super(message)
    this.name = "ApiError"
    this.status = status
    this.retryAt = retryAt
  }
}

let onUnauthorized: () => void = () => {}

export function setUnauthorizedHandler(fn: () => void): void {
  onUnauthorized = fn
}

export function notifyUnauthorized(): void {
  onUnauthorized()
}

export async function request<T>(method: string, path: string, body?: unknown, signal?: AbortSignal): Promise<T> {
  const headers: Record<string, string> = { "X-GHR": "1" }
  let payload: string | undefined
  if (typeof body === "string") {
    headers["Content-Type"] = "text/plain"
    payload = body
  } else if (body !== undefined) {
    headers["Content-Type"] = "application/json"
    payload = JSON.stringify(body)
  }
  const res = await fetch(path, { method, headers, credentials: "same-origin", body: payload, signal })
  if (res.ok) {
    if (res.status === 202 || res.status === 204) return undefined as T
    return (await res.json()) as T
  }
  const err = await toError(res)
  // A 401 from the API or /auth/password means "log in again"; one from
  // /auth/login or /auth/setup is a wrong password or setup code the form
  // shows. /auth/password answers a wrong current password with 403.
  if (res.status === 401 && (path.startsWith("/api/") || path === "/auth/password")) onUnauthorized()
  throw err
}

// A download whose body is the file itself, not JSON.
async function requestBlob(path: string): Promise<Blob> {
  const res = await fetch(path, { method: "GET", headers: { "X-GHR": "1" }, credentials: "same-origin" })
  if (res.ok) return res.blob()
  const err = await toError(res)
  if (res.status === 401) onUnauthorized()
  throw err
}

async function toError(res: Response): Promise<ApiError> {
  const text = await res.text()
  let message = text.trim() || `${res.status} ${res.statusText}`.trim()
  let retryAt: Date | undefined
  try {
    const parsed = JSON.parse(text) as { error?: unknown; retry_at?: unknown }
    if (typeof parsed.error === "string") message = parsed.error
    if (typeof parsed.retry_at === "string") retryAt = new Date(parsed.retry_at)
  } catch {
    // a plain-text body keeps its text as the message
  }
  return new ApiError(res.status, message, retryAt)
}

const query = (params: Record<string, string | number>) =>
  new URLSearchParams(Object.entries(params).map(([k, v]) => [k, String(v)])).toString()
const seg = encodeURIComponent
const send = (method: string, path: string, body?: unknown): Promise<void> => request<undefined>(method, path, body)

export const api = {
  authState: (signal?: AbortSignal) => request<AuthState>("GET", "/auth/state", undefined, signal),
  setup: (code: string, password: string) => send("POST", "/auth/setup", { code, password }),
  login: (password: string) => send("POST", "/auth/login", { password }),
  logout: () => send("POST", "/auth/logout"),
  changePassword: (current: string, next: string) => send("POST", "/auth/password", { current, new: next }),
  setupState: (signal?: AbortSignal) => request<SetupState>("GET", "/api/setup", undefined, signal),
  setupGitHub: (owner: string, token: string) => send("POST", "/api/setup/github", { owner, token }),
  setupFinish: (toolchains: "popular" | "none") => send("POST", "/api/setup/finish", { toolchains }),
  status: (signal?: AbortSignal) => request<Status>("GET", "/api/status", undefined, signal),
  events: (after: number, signal?: AbortSignal) =>
    request<GhrEvent[]>("GET", `/api/events?${query({ after })}`, undefined, signal),
  history: (repo: string, conclusion: string, since: string, limit: number, signal?: AbortSignal) =>
    request<HistoryEntry[]>("GET", `/api/history?${query({ repo, conclusion, ...(since ? { since } : {}), limit })}`, undefined, signal),
  log: (id: string, cursor: string, signal?: AbortSignal) =>
    request<LogChunk>("GET", `/api/runners/${seg(id)}/log?${query({ cursor })}`, undefined, signal),
  steps: (id: string, signal?: AbortSignal) => request<Step[]>("GET", `/api/runners/${seg(id)}/steps`, undefined, signal),
  containers: (id: string, signal?: AbortSignal) =>
    request<Container[]>("GET", `/api/runners/${seg(id)}/containers`, undefined, signal),
  metrics: (signal?: AbortSignal) => request<Metrics>("GET", "/api/metrics", undefined, signal),
  activity: (window: ActivityWindow, tz: string, repo: string, signal?: AbortSignal) =>
    request<Activity>("GET", `/api/activity?${query({ window, tz, ...(repo ? { repo } : {}) })}`, undefined, signal),
  actions: (signal?: AbortSignal) => request<Actions>("GET", "/api/actions", undefined, signal),
  runDetail: (repo: string, id: number, attempt: number | undefined, signal?: AbortSignal) =>
    request<RunDetail>("GET", `/api/actions/${seg(repo)}/runs/${id}${attempt ? `?${query({ attempt })}` : ""}`, undefined, signal),
  runAnnotations: (repo: string, id: number, attempt: number | undefined, signal?: AbortSignal) =>
    request<RunAnnotations>(
      "GET",
      `/api/actions/${seg(repo)}/runs/${id}/annotations${attempt ? `?${query({ attempt })}` : ""}`,
      undefined,
      signal,
    ),
  jobLog: (repo: string, jobId: number, signal?: AbortSignal) =>
    request<JobLog>("GET", `/api/actions/${seg(repo)}/jobs/${jobId}/log`, undefined, signal),
  rawJobLog: (repo: string, jobId: number) => requestBlob(`/api/actions/${seg(repo)}/jobs/${jobId}/log/raw`),
  artifactDownload: (repo: string, id: number) => request<DownloadURL>("GET", `/api/actions/${seg(repo)}/artifacts/${id}/download`),
  config: (signal?: AbortSignal) => request<Config>("GET", "/api/config", undefined, signal),
  stopRunner: (id: string) => send("DELETE", `/api/runners/${seg(id)}`),
  pauseAll: () => send("POST", "/api/pause-all"),
  resumeAll: () => send("POST", "/api/resume-all"),
  patchConfig: (patch: ConfigPatch) => send("PATCH", "/api/config", patch),
  reload: () => request<string[]>("POST", "/api/reload"),
  restart: () => send("POST", "/api/restart"),
  availableRepos: (signal?: AbortSignal) => request<AvailableRepo[]>("GET", "/api/repos/available", undefined, signal),
  addRepo: (req: AddRepoRequest) => send("POST", "/api/repos", req),
  removeRepo: (name: string) => send("DELETE", `/api/repos/${seg(name)}`),
  watchRepo: (name: string) => send("POST", "/api/watch", { name }),
  unwatchRepo: (name: string) => send("DELETE", `/api/watch/${seg(name)}`),
  pauseRepo: (name: string) => send("POST", `/api/repos/${seg(name)}/pause`),
  resumeRepo: (name: string) => send("POST", `/api/repos/${seg(name)}/resume`),
  labelCheck: (name: string, signal?: AbortSignal) =>
    request<LabelCheck>("GET", `/api/repos/${seg(name)}/label-check`, undefined, signal),
  startLabelCheck: (name: string) => send("POST", `/api/repos/${seg(name)}/label-check`),
  registrations: (name: string, signal?: AbortSignal) =>
    request<Registration[]>("GET", `/api/repos/${seg(name)}/registrations`, undefined, signal),
  deleteRegistration: (name: string, id: number) => send("DELETE", `/api/repos/${seg(name)}/registrations/${id}`),
  token: (signal?: AbortSignal) => request<TokenStatus>("GET", "/api/token", undefined, signal),
  replaceToken: (token: string) => send("PUT", "/api/token", token),
  queueRunnerUpdate: () => send("POST", "/api/runner-update"),
  cancelRunnerUpdate: () => send("DELETE", "/api/runner-update"),
  storage: (signal?: AbortSignal) => request<Storage>("GET", "/api/storage", undefined, signal),
  refreshStorage: () => send("POST", "/api/storage/refresh"),
  toolchainChoices: (tool: string, signal?: AbortSignal) =>
    request<ToolchainChoice[]>("GET", `/api/toolchains/available?${query({ tool })}`, undefined, signal),
  installToolchain: (tool: string, version: string) => send("POST", "/api/toolchains", { tool, version }),
  installPreset: (preset: string) => send("POST", "/api/toolchains", { preset }),
  removeToolchain: (tool: string, version: string) => send("DELETE", `/api/toolchains/${seg(tool)}/${seg(version)}`),
  clearCache: (name: string) => send("POST", `/api/caches/${seg(name)}/clear`),
  prune: (scope: PruneScope) => send("POST", scope === "standard" ? "/api/prune" : `/api/prune/${seg(scope)}`),
  githubStorage: (signal?: AbortSignal) => request<GitHubStorage>("GET", "/api/github-storage", undefined, signal),
  refreshGitHubStorage: () => send("POST", "/api/github-storage/refresh"),
  storagePreview: (cleanup: string, repo?: string, signal?: AbortSignal) =>
    request<StoragePreview[]>("GET", `/api/github-storage/preview?${query({ cleanup, ...(repo ? { repo } : {}) })}`, undefined, signal),
  deleteArtifact: (repo: string, id: number) => send("DELETE", `/api/github-storage/${seg(repo)}/artifacts/${id}`),
  deleteCache: (repo: string, id: number) => send("DELETE", `/api/github-storage/${seg(repo)}/caches/${id}`),
  // The bulk deletes answer 202; their progress shows in the snapshot.
  deleteArtifacts: (repo: string, body: { ids: number[] } | { older_than: string }) =>
    send("POST", `/api/github-storage/${seg(repo)}/artifacts/delete`, body),
  deleteCaches: (repo: string, body: { ids: number[] } | { all: true }) =>
    send("POST", `/api/github-storage/${seg(repo)}/caches/delete`, body),
  setRetention: (repo: string, days: number) => send("PUT", `/api/github-storage/${seg(repo)}/retention`, { days }),
  setRetentionAll: (days: number) => send("POST", "/api/github-storage/retention", { days }),
}
