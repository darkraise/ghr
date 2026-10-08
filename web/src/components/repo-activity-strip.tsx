import type { ActivityHour } from "@/api/types"

type Cell = "none" | "ok-1" | "ok-2" | "ok-3" | "bad"

const CELL_CLASS: Record<Cell, string> = {
  none: "bg-muted",
  "ok-1": "bg-success/35",
  "ok-2": "bg-success/65",
  "ok-3": "bg-success",
  bad: "bg-destructive",
}

function cellOf(h: ActivityHour): Cell {
  if (h.failed > 0) return "bad"
  const runs = h.succeeded + h.cancelled
  if (runs === 0) return "none"
  return runs === 1 ? "ok-1" : runs <= 3 ? "ok-2" : "ok-3"
}

export function RepoActivityStrip({ repo, hours }: { repo: string; hours: ActivityHour[] }) {
  const ok = hours.reduce((n, h) => n + h.succeeded, 0)
  const failed = hours.reduce((n, h) => n + h.failed, 0)
  const label = ok + failed === 0 ? `${repo}, last 24 hours: no runs` : `${repo}, last 24 hours: ${ok} succeeded, ${failed} failed`
  return (
    <div role="img" aria-label={label} className="flex h-4 items-stretch gap-px">
      {hours.map((h, i) => {
        const cell = cellOf(h)
        const open = i === hours.length - 1
        return (
          <span
            key={h.start}
            data-cell={cell}
            data-open={open ? "true" : undefined}
            className={`w-1.5 rounded-[1px] ${CELL_CLASS[cell]} ${open ? "outline outline-1 -outline-offset-1 outline-primary" : ""}`}
          />
        )
      })}
    </div>
  )
}
