import { useQuery, useQueryClient } from "@tanstack/react-query"
import { api } from "./client"
import type { ActivityWindow, GhrEvent, LogChunk, Status, Storage } from "./types"

export const POLL_FAST = 1000
export const POLL_SLOW = 5000
export const MAX_EVENTS = 200
export const MAX_LOG = 256 * 1024

export const keys = {
  auth: ["auth"] as const,
  setup: ["setup"] as const,
  status: ["status"] as const,
  config: ["config"] as const,
  metrics: ["metrics"] as const,
  activity: (window: string, tz: string) => ["activity", window, tz] as const,
  events: (epoch: string) => ["events", epoch] as const,
  history: (repo: string, conclusion: string) => ["history", repo, conclusion] as const,
  steps: (id: string) => ["steps", id] as const,
  containers: (id: string) => ["containers", id] as const,
  log: (id: string) => ["log", id] as const,
  token: ["token"] as const,
  storage: ["storage"] as const,
  availableRepos: ["available-repos"] as const,
  repoActivity: (name: string) => ["repo-activity", name] as const,
  labelCheck: (name: string) => ["label-check", name] as const,
  registrations: (name: string) => ["registrations", name] as const,
  toolchainChoices: (tool: string) => ["toolchain-choices", tool] as const,
}

export function useStatus() {
  return useQuery({ queryKey: keys.status, queryFn: ({ signal }) => api.status(signal), refetchInterval: POLL_FAST })
}

// How long the wizard waits for ghr to start after saving owner and token;
// tests shorten it.
export const setupStart = { limitMs: 60_000 }

// Polls while the caller waits for ghr to start, or while the daemon says it
// is starting (a reload in the middle of it).
export function useSetupState(poll: boolean) {
  return useQuery({
    queryKey: keys.setup,
    queryFn: ({ signal }) => api.setupState(signal),
    refetchInterval: (q) => (poll || q.state.data?.starting ? POLL_FAST : false),
  })
}

export function useConfig() {
  return useQuery({ queryKey: keys.config, queryFn: ({ signal }) => api.config(signal), refetchInterval: POLL_SLOW })
}

export function useMetrics() {
  return useQuery({ queryKey: keys.metrics, queryFn: ({ signal }) => api.metrics(signal), refetchInterval: POLL_SLOW })
}

export function browserZone(): string {
  return Intl.DateTimeFormat().resolvedOptions().timeZone
}

// The daemon aligns buckets to the zone it is given, so the browser sends its
// own and the columns line up with the viewer's clock.
export function useActivity(window: ActivityWindow) {
  const tz = browserZone()
  return useQuery({
    queryKey: keys.activity(window, tz),
    queryFn: ({ signal }) => api.activity(window, tz, signal),
    refetchInterval: POLL_SLOW,
  })
}

export function useHistory(repo: string, conclusion: string) {
  return useQuery({
    queryKey: keys.history(repo, conclusion),
    queryFn: ({ signal }) => api.history(repo, conclusion, 200, signal),
    refetchInterval: POLL_SLOW,
  })
}

export function useSteps(id: string, live: boolean) {
  return useQuery({
    queryKey: keys.steps(id),
    queryFn: ({ signal }) => api.steps(id, signal),
    enabled: live,
    refetchInterval: live ? POLL_SLOW : false,
  })
}

export function useContainers(id: string, live: boolean) {
  return useQuery({
    queryKey: keys.containers(id),
    queryFn: ({ signal }) => api.containers(id, signal),
    enabled: live,
    refetchInterval: live ? POLL_SLOW : false,
  })
}

export function appendEvents(prev: GhrEvent[], next: GhrEvent[]): GhrEvent[] {
  const last = prev.at(-1)?.seq ?? 0
  const fresh = next.filter((e) => e.seq > last)
  if (fresh.length === 0) return prev
  return [...prev, ...fresh].slice(-MAX_EVENTS)
}

// The epoch is part of the key: a daemon restart restarts the sequence
// numbers, so a new epoch starts an empty list with the cursor at 0.
export function useEvents(epoch: string | undefined): GhrEvent[] {
  const queryClient = useQueryClient()
  const key = keys.events(epoch ?? "")
  const q = useQuery({
    queryKey: key,
    queryFn: async ({ signal }) => {
      const prev = queryClient.getQueryData<GhrEvent[]>(key) ?? []
      return appendEvents(prev, await api.events(prev.at(-1)?.seq ?? 0, signal))
    },
    enabled: epoch !== undefined,
    refetchInterval: POLL_FAST,
  })
  return q.data ?? []
}

export interface LogTail {
  text: string
  next: string
}

export function appendLog(prev: LogTail, chunk: LogChunk): LogTail {
  const text = prev.text + chunk.data
  return { text: text.length > MAX_LOG ? text.slice(-MAX_LOG) : text, next: chunk.next }
}

export function useLogTail(id: string, enabled: boolean) {
  const queryClient = useQueryClient()
  const key = keys.log(id)
  return useQuery({
    queryKey: key,
    queryFn: async ({ signal }) => {
      const prev = queryClient.getQueryData<LogTail>(key) ?? { text: "", next: "" }
      return appendLog(prev, await api.log(id, prev.next, signal))
    },
    enabled,
    refetchInterval: enabled ? POLL_FAST : false,
  })
}

export const ACTIVITY_LIMIT = 500

export function useToken() {
  return useQuery({ queryKey: keys.token, queryFn: ({ signal }) => api.token(signal), refetchInterval: POLL_SLOW })
}

// A prune shows in /status, not /storage; either one running makes the
// Storage and Toolchains pages poll every second.
export function storageBusy(storage?: Storage, status?: Status): boolean {
  if (status?.maintenance.running) return true
  if (!storage) return false
  return storage.operations.current !== null || storage.operations.queued > 0 || storage.measuring
}

export function useStorage(status: Status | undefined) {
  return useQuery({
    queryKey: keys.storage,
    queryFn: ({ signal }) => api.storage(signal),
    refetchInterval: (q) => (storageBusy(q.state.data, status) ? POLL_FAST : POLL_SLOW),
  })
}

export function useRepoActivity(name: string) {
  return useQuery({
    queryKey: keys.repoActivity(name),
    queryFn: ({ signal }) => api.history(name, "", ACTIVITY_LIMIT, signal),
    refetchInterval: POLL_SLOW,
  })
}

export function useLabelCheck(name: string, enabled: boolean) {
  return useQuery({
    queryKey: keys.labelCheck(name),
    queryFn: ({ signal }) => api.labelCheck(name, signal),
    enabled,
    refetchInterval: (q) => (enabled && q.state.data?.state === "checking" ? POLL_FAST : false),
  })
}

export function useRegistrations(name: string, enabled: boolean) {
  return useQuery({
    queryKey: keys.registrations(name),
    queryFn: ({ signal }) => api.registrations(name, signal),
    enabled,
  })
}

export function useAvailableRepos(enabled: boolean) {
  return useQuery({ queryKey: keys.availableRepos, queryFn: ({ signal }) => api.availableRepos(signal), enabled, staleTime: 0 })
}

export function useToolchainChoices(tool: string, enabled: boolean) {
  return useQuery({
    queryKey: keys.toolchainChoices(tool),
    queryFn: ({ signal }) => api.toolchainChoices(tool, signal),
    enabled: enabled && tool !== "",
  })
}
