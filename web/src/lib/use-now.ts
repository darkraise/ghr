import { useQueryClient } from "@tanstack/react-query"
import { useEffect, useSyncExternalStore } from "react"
import { keys } from "@/api/hooks"
import type { Status } from "@/api/types"

// Elapsed times and ages compare against timestamps the daemon wrote, so they
// run on the daemon's clock: a browser clock that is off would skew them all.
export function clockOffset(status: Status | undefined, receivedAt: number): number {
  const server = status ? Date.parse(status.now) : Number.NaN
  return Number.isNaN(server) ? 0 : server - receivedAt
}

function useClockOffset(): number {
  const queryClient = useQueryClient()
  return useSyncExternalStore(
    (onChange) => queryClient.getQueryCache().subscribe(onChange),
    () => {
      const state = queryClient.getQueryState<Status>(keys.status)
      return clockOffset(state?.data, state?.dataUpdatedAt ?? 0)
    },
  )
}

// One ticker serves every useNow caller, and runs only while one is mounted.
let now = Date.now()
let timer: ReturnType<typeof setInterval> | undefined
const listeners = new Set<() => void>()

function read(): void {
  const at = Date.now()
  if (at === now) return
  now = at
  for (const l of listeners) l()
}

function subscribe(l: () => void): () => void {
  listeners.add(l)
  timer ??= setInterval(read, 1000)
  return () => {
    listeners.delete(l)
    if (listeners.size > 0) return
    clearInterval(timer)
    timer = undefined
  }
}

// Nothing ticks while nobody listens, so the first caller to render would
// find the time the last one left. Within a second of it the value is kept,
// because a snapshot must not change between two reads of one render.
function snapshot(): number {
  if (timer === undefined) {
    const at = Date.now()
    if (Math.abs(at - now) >= 1000) now = at
  }
  return now
}

export function useNow(): number {
  const offset = useClockOffset()
  const at = useSyncExternalStore(subscribe, snapshot)
  // Reading the clock on a new offset keeps the tick from lagging behind the
  // status that set it, which could read a check made a minute ago as "just now".
  useEffect(read, [offset])
  return at + offset
}
