import { Check, TriangleAlert, X, type LucideIcon } from "lucide-react"
import type { GhrEvent } from "@/api/types"
import { hhmm } from "@/lib/format"

const LEVELS: Record<string, { label: string; Icon: LucideIcon; className: string } | undefined> = {
  ok: { label: "OK", Icon: Check, className: "text-success" },
  warn: { label: "Warning", Icon: TriangleAlert, className: "text-warning" },
  error: { label: "Error", Icon: X, className: "text-destructive" },
}

export function EventList({ events, limit = 8 }: { events: GhrEvent[]; limit?: number }) {
  const shown = events.slice(-limit).reverse()
  if (shown.length === 0) return <p className="py-2 text-sm text-muted-foreground">No events yet.</p>
  return (
    <ul className="flex flex-col text-sm">
      {shown.map((e) => {
        const level = LEVELS[e.level]
        return (
          <li key={e.seq} className="grid grid-cols-[3rem_1rem_minmax(0,1fr)] items-start gap-2 border-b border-border py-1.5 last:border-b-0">
            <time dateTime={e.time} className="font-mono text-muted-foreground">
              {hhmm(new Date(e.time))}
            </time>
            <span className="pt-0.5">{level && <level.Icon role="img" aria-label={level.label} size={15} className={level.className} />}</span>
            <span className="break-words">
              {e.repo && <span className="text-muted-foreground">{e.repo} </span>}
              <span>{e.msg}</span>
            </span>
          </li>
        )
      })}
    </ul>
  )
}
