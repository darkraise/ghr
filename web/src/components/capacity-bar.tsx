import type { Status } from "@/api/types"
import { capacity, type SegmentKind } from "@/lib/capacity"

// A busy runner's light is the one other authored motion (spec 1.4).
const KIND_CLASS: Record<Exclude<SegmentKind, "free">, string> = {
  busy: "ghr-pulse bg-running",
  starting: "bg-running/45",
  warm: "border border-running",
}

export function CapacitySegments({
  status,
  size = "sm",
}: {
  status: Status
  size?: "sm" | "lg"
}) {
  const c = capacity(status)
  const firstOver = c.segments.length - c.over
  return (
    <div role="img" aria-label={c.label} className={`flex ${size === "lg" ? "h-3 gap-1.5" : "h-2 gap-1"}`}>
      {c.segments.map((kind, i) => (
        <span
          key={i}
          data-kind={kind}
          className={`min-w-1 flex-1 rounded-[2px] ${kind === "free" ? "bg-muted" : KIND_CLASS[kind]} ${c.over > 0 && i === firstOver ? "ml-[2px]" : ""}`}
        />
      ))}
    </div>
  )
}
