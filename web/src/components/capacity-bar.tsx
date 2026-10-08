import type { Status } from "@/api/types"
import { capacity, type SegmentKind } from "@/lib/capacity"

// A busy runner's light is the one other authored motion (spec 1.4).
const KIND_CLASS: Record<Exclude<SegmentKind, "free">, string> = {
  busy: "ghr-pulse bg-primary",
  starting: "bg-primary/45",
  warm: "border border-primary",
}

export function CapacitySegments({
  status,
  size = "sm",
  tone = "card",
}: {
  status: Status
  size?: "sm" | "lg"
  tone?: "card" | "rail"
}) {
  const c = capacity(status)
  const free = tone === "rail" ? "bg-[hsl(var(--surface-sidebar))]" : "bg-muted"
  const firstOver = c.segments.length - c.over
  return (
    <div role="img" aria-label={c.label} className={`flex ${size === "lg" ? "h-3 gap-1.5" : "h-2 gap-1"}`}>
      {c.segments.map((kind, i) => (
        <span
          key={i}
          data-kind={kind}
          className={`min-w-1 flex-1 rounded-[2px] ${kind === "free" ? free : KIND_CLASS[kind]} ${c.over > 0 && i === firstOver ? "ml-[2px]" : ""}`}
        />
      ))}
    </div>
  )
}

export function CapacityBar({ status }: { status: Status }) {
  const c = capacity(status)
  return (
    <div className="flex flex-col gap-2 rounded-[6px] bg-[hsl(var(--sidebar-hover-bg))] p-2.5">
      <div className="ghr-rail-wide flex items-baseline justify-between gap-2 text-sm">
        <span className="text-[hsl(var(--sidebar-foreground-muted))]">Runners</span>
        <span className="font-mono text-[hsl(var(--sidebar-foreground))]">{c.text}</span>
      </div>
      <CapacitySegments status={status} tone="rail" />
    </div>
  )
}
