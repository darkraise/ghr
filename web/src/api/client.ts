import type {
  AddRepoRequest,
  AuthState,
  AvailableRepo,
  Config,
  ConfigPatch,
  Container,
  GhrEvent,
  HistoryEntry,
  LabelCheck,
  LogChunk,
  Metrics,
  PruneScope,
  Registration,
  SetupState,
  Status,
  Step,
  Storage,
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
  // Only an expired session on the API means "log in again"; a 401 from
  // /auth/login or /auth/password is a wrong password the form shows.
  if (res.status === 401 && path.startsWith("/api/")) onUnauthorized()
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
  setup: (password: string) => send("POST", "/auth/setup", { password }),
  login: (password: string) => send("POST", "/auth/login", { password }),
  logout: () => send("POST", "/auth/logout"),
  changePassword: (current: string, next: string) => send("POST", "/auth/password", { current, new: next }),
  setupState: (signal?: AbortSignal) => request<SetupState>("GET", "/api/setup", undefined, signal),
  setupGitHub: (owner: string, token: string) => send("POST", "/api/setup/github", { owner, token }),
  setupFinish: (toolchains: "popular" | "none") => send("POST", "/api/setup/finish", { toolchains }),
  status: (signal?: AbortSignal) => request<Status>("GET", "/api/status", undefined, signal),
  events: (after: number, signal?: AbortSignal) =>
    request<GhrEvent[]>("GET", `/api/events?${query({ after })}`, undefined, signal),
  history: (repo: string, conclusion: string, limit: number, signal?: AbortSignal) =>
    request<HistoryEntry[]>("GET", `/api/history?${query({ repo, conclusion, limit })}`, undefined, signal),
  log: (id: string, cursor: string, signal?: AbortSignal) =>
    request<LogChunk>("GET", `/api/runners/${seg(id)}/log?${query({ cursor })}`, undefined, signal),
  steps: (id: string, signal?: AbortSignal) => request<Step[]>("GET", `/api/runners/${seg(id)}/steps`, undefined, signal),
  containers: (id: string, signal?: AbortSignal) =>
    request<Container[]>("GET", `/api/runners/${seg(id)}/containers`, undefined, signal),
  metrics: (signal?: AbortSignal) => request<Metrics>("GET", "/api/metrics", undefined, signal),
  config: (signal?: AbortSignal) => request<Config>("GET", "/api/config", undefined, signal),
  stopRunner: (id: string) => send("DELETE", `/api/runners/${seg(id)}`),
  pauseAll: () => send("POST", "/api/pause-all"),
  resumeAll: () => send("POST", "/api/resume-all"),
  patchConfig: (patch: ConfigPatch) => send("PATCH", "/api/config", patch),
  reload: () => request<string[]>("POST", "/api/reload"),
  availableRepos: (signal?: AbortSignal) => request<AvailableRepo[]>("GET", "/api/repos/available", undefined, signal),
  addRepo: (req: AddRepoRequest) => send("POST", "/api/repos", req),
  removeRepo: (name: string) => send("DELETE", `/api/repos/${seg(name)}`),
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
}
