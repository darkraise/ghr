import { useQueryClient } from "@tanstack/react-query"
import { useEffect, useState, useSyncExternalStore } from "react"
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

export function useNow(): number {
  const [now, setNow] = useState(() => Date.now())
  const offset = useClockOffset()
  // Restarting on a new offset keeps the tick from lagging behind the status
  // that set it, which could read a check made a minute ago as "just now".
  useEffect(() => {
    setNow(Date.now())
    const timer = setInterval(() => setNow(Date.now()), 1000)
    return () => clearInterval(timer)
  }, [offset])
  return now + offset
}
