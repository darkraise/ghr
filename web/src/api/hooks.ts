import { useQuery, useQueryClient } from "@tanstack/react-query"
import { api } from "./client"
import type { GhrEvent, LogChunk } from "./types"

export const POLL_FAST = 1000
export const POLL_SLOW = 5000
export const MAX_EVENTS = 200
export const MAX_LOG = 256 * 1024

export const keys = {
  auth: ["auth"] as const,
  status: ["status"] as const,
  config: ["config"] as const,
  metrics: ["metrics"] as const,
  events: (epoch: string) => ["events", epoch] as const,
  history: (repo: string, conclusion: string) => ["history", repo, conclusion] as const,
  steps: (id: string) => ["steps", id] as const,
  containers: (id: string) => ["containers", id] as const,
  log: (id: string) => ["log", id] as const,
}

export function useStatus() {
  return useQuery({ queryKey: keys.status, queryFn: ({ signal }) => api.status(signal), refetchInterval: POLL_FAST })
}

export function useConfig() {
  return useQuery({ queryKey: keys.config, queryFn: ({ signal }) => api.config(signal), refetchInterval: POLL_SLOW })
}

export function useMetrics() {
  return useQuery({ queryKey: keys.metrics, queryFn: ({ signal }) => api.metrics(signal), refetchInterval: POLL_SLOW })
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
