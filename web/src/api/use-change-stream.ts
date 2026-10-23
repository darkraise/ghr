import { useQueryClient, type QueryClient, type QueryKey } from "@tanstack/react-query"
import { useEffect } from "react"
import { keys } from "./hooks"
import { liveConfig, runChanges, setLive } from "./live"

// The queries each change topic makes stale. A one-element key is a prefix:
// it matches every window, zone, repository, filter and epoch.
export const TOPIC_QUERIES: Record<string, readonly QueryKey[]> = {
  status: [keys.status, ["activity"]],
  gauges: [keys.status, keys.token],
  events: [["events"]],
  // The snapshots' cleanup and covered columns follow the config, and so
  // does the managed package list.
  config: [keys.config, ["activity"], keys.githubStorage, keys.artifactStore, keys.packages],
  token: [keys.token],
  storage: [keys.storage],
  "github-storage": [keys.githubStorage],
  // Also a prefix of every run page's RustFS rows.
  "artifact-store": [keys.artifactStore],
  // A prefix: it also matches every package search.
  packages: [keys.packages],
  history: [["history"], ["activity"]],
  metrics: [keys.metrics, ["activity"]],
}

export function invalidateTopics(qc: QueryClient, topics: readonly string[]): void {
  const seen = new Set<string>()
  for (const topic of topics) {
    for (const key of TOPIC_QUERIES[topic] ?? []) {
      const id = JSON.stringify(key)
      if (seen.has(id)) continue
      seen.add(id)
      void qc.invalidateQueries({ queryKey: key })
    }
  }
}

export function invalidateAll(qc: QueryClient): void {
  invalidateTopics(qc, Object.keys(TOPIC_QUERIES))
}

// useChangeStream keeps the daemon's change stream open while the tab is
// visible and refetches what each notice names. Hiding the tab closes it, so
// background tabs neither refetch nor hold one of the browser's six
// connections to the host.
export function useChangeStream(): void {
  const queryClient = useQueryClient()
  useEffect(() => {
    if (!liveConfig.enabled) return
    let current: AbortController | undefined
    // True while a change may have gone unannounced: the page loaded hidden,
    // an attempt ended, or the stream was stopped for a hidden tab. The hello
    // carries no snapshot, so the next one refetches everything.
    let missed = document.visibilityState === "hidden"
    const start = () => {
      const ctrl = new AbortController()
      current = ctrl
      // An older loop's late callbacks must not touch a newer connection's state.
      const mine = () => current === ctrl
      void runChanges(
        {
          onHello: () => {
            if (!mine()) return
            if (missed) invalidateAll(queryClient)
            missed = false
            setLive(true)
          },
          onChange: (topics) => {
            if (mine()) invalidateTopics(queryClient, topics)
          },
          onEnd: () => {
            if (!mine()) return
            missed = true
            setLive(false)
          },
        },
        ctrl.signal,
      )
    }
    const stop = () => {
      current?.abort()
      current = undefined
      missed = true
      setLive(false)
    }
    const onVisibility = () => {
      if (document.visibilityState === "hidden") stop()
      else if (!current) start()
    }
    document.addEventListener("visibilitychange", onVisibility)
    if (document.visibilityState !== "hidden") start()
    return () => {
      document.removeEventListener("visibilitychange", onVisibility)
      stop()
    }
  }, [queryClient])
}
