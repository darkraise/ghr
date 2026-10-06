import type { AuthState, Config, Container, GhrEvent, HistoryEntry, LogChunk, Metrics, Status, Step } from "./types"

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
  if (body !== undefined) headers["Content-Type"] = "application/json"
  const res = await fetch(path, {
    method,
    headers,
    credentials: "same-origin",
    body: body === undefined ? undefined : JSON.stringify(body),
    signal,
  })
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
}
