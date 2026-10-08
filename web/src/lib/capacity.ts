import type { Status } from "@/api/types"

export type SegmentKind = "busy" | "starting" | "warm" | "free"

export interface Capacity {
  segments: SegmentKind[]
  over: number
  busy: number
  text: string
  label: string
}

// A cleaning instance no longer counts against global_max (sched.go), so it
// has no segment.
const KIND: Record<string, SegmentKind | undefined> = { busy: "busy", starting: "starting", idle: "warm" }
const ORDER: SegmentKind[] = ["busy", "starting", "warm"]

export function capacity(status: Status): Capacity {
  const live = status.instances.flatMap((i) => {
    const kind = KIND[i.state]
    return kind ? [kind] : []
  })
  const count = (kind: SegmentKind) => live.filter((k) => k === kind).length
  const counted = ORDER.flatMap((kind) => Array<SegmentKind>(count(kind)).fill(kind))
  const busy = count("busy")
  const extra = [count("starting") > 0 && `${count("starting")} starting`, count("warm") > 0 && `${count("warm")} warm`]
  if (status.mode === "all") {
    const text = `${busy} busy`
    return { segments: counted, over: 0, busy, text, label: [text, ...extra].filter(Boolean).join(", ") }
  }
  const max = status.global_max
  const free = Math.max(0, max - counted.length)
  const over = Math.max(0, counted.length - max)
  const text = over > 0 ? `${busy} busy, max ${max}` : `${busy} of ${max} busy`
  const label = [text, ...extra, free > 0 && `${free} free`].filter(Boolean).join(", ")
  return { segments: [...counted, ...Array<SegmentKind>(free).fill("free")], over, busy, text, label }
}
