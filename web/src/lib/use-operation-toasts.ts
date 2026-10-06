import { toast } from "darkraise-ui/components/sonner"
import { useEffect, useRef } from "react"
import type { Storage } from "@/api/types"

// One toast per snapshot names every operation that finished since the last
// one, failures first. The first snapshot only records where the page starts,
// so opening it does not replay old results.
export function useOperationToasts(storage: Storage | undefined) {
  const last = useRef<string | null | undefined>(undefined)
  useEffect(() => {
    if (!storage) return
    const recent = storage.operations.recent ?? []
    const newest = recent[0]?.id ?? null
    if (last.current === undefined || newest === null || newest === last.current) {
      if (last.current === undefined) last.current = newest
      return
    }
    const seen = recent.findIndex((o) => o.id === last.current)
    const fresh = seen === -1 ? recent : recent.slice(0, seen)
    last.current = newest
    const bad: string[] = []
    const good: string[] = []
    for (const o of fresh) {
      const text = `${o.kind} ${o.target}: ${o.message || o.outcome || ""}`
      if (o.outcome === "ok" || o.outcome === "skipped") good.push(text)
      else bad.push(text)
    }
    const text = [...bad, ...good].join(" · ")
    if (bad.length > 0) toast.error(text)
    else toast.success(text)
  }, [storage])
}
