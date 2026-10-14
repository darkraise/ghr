import type { Status } from "@/api/types"
import { capacity, type SegmentKind } from "@/lib/capacity"

// A busy runner's light is the one other authored motion (spec 1.4).
const KIND_CLASS: Record<Exclude<SegmentKind, "free">, string> = {
  busy: "ghr-pulse bg-running",
  starting: "bg-running/45",
  warm: "border border-running",
}

// Past this many segments a per-runner cell is too narrow to tell apart, so
// runs of one kind are drawn as a single proportional span.
const MAX_SEGMENTS = 16

interface Group {
  kind: SegmentKind
  count: number
  start: number
}

function grouped(kinds: SegmentKind[], firstOver: number): Group[] {
  const groups: Group[] = []
  kinds.forEach((kind, i) => {
    const last = groups.at(-1)
    if (last && last.kind === kind && i !== firstOver) last.count++
    else groups.push({ kind, count: 1, start: i })
  })
  return groups
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
  const many = c.segments.length > MAX_SEGMENTS
  const groups = many
    ? grouped(c.segments, c.over > 0 ? firstOver : -1)
    : c.segments.map((kind, i) => ({ kind, count: 1, start: i }))
  return (
    <div role="img" aria-label={c.label} className={`flex ${size === "lg" ? "h-3 gap-1.5" : "h-2 gap-1"}`}>
      {groups.map((g) => (
        <span
          key={g.start}
          data-kind={g.kind}
          style={many ? { flexGrow: g.count } : undefined}
          className={`${many ? "" : "min-w-1 "}flex-1 rounded-[2px] ${g.kind === "free" ? "bg-muted" : KIND_CLASS[g.kind]} ${c.over > 0 && g.start === firstOver ? "ml-[2px]" : ""}`}
        />
      ))}
    </div>
  )
}
