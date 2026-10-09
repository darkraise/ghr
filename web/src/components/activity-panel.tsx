import { Button } from "darkraise-ui/components/button"
import type { ReactNode } from "react"
import type { Activity, ActivityWindow } from "@/api/types"
import { BucketsChart } from "@/components/buckets-chart"
import { LanesChart } from "@/components/lanes-chart"
import { WindowControl } from "@/components/window-control"
import { activitySummary, bucketAria, isLaneWindow, rangeText, runAria } from "@/lib/activity-view"
import { useWidth } from "@/lib/use-width"
import { errorText } from "@/query"

function Swatch({ className, dash = false }: { className: string; dash?: boolean }) {
  return (
    <svg width="14" height="10" aria-hidden="true" className="shrink-0">
      <rect x="0.5" y="0.5" width="13" height="9" rx="2" strokeWidth="1" strokeDasharray={dash ? "3 2" : undefined} className={className} />
    </svg>
  )
}

function LineSwatch({ className, dash = false }: { className: string; dash?: boolean }) {
  return (
    <svg width="14" height="10" aria-hidden="true" className="shrink-0">
      <line x1="0" y1="5" x2="14" y2="5" strokeWidth="1.5" strokeDasharray={dash ? "3 2" : undefined} className={className} />
    </svg>
  )
}

// Each state reads by shape as well as colour: solid, dashed outline, plain
// outline, or tint.
function Legend({ lanes, capacity }: { lanes: boolean; capacity: number | null }) {
  const items: [string, ReactNode][] = lanes
    ? [
        ["Succeeded", <Swatch className="fill-success/30 stroke-success/60" />],
        ["Failed", <Swatch className="fill-destructive/30 stroke-destructive/70" />],
        ["Cancelled", <Swatch className="fill-muted stroke-muted-foreground" />],
        ["Unknown", <Swatch className="fill-muted stroke-muted-foreground" dash />],
        ["Running", <Swatch className="fill-primary stroke-primary" />],
        ["Finishing", <Swatch className="fill-primary/25 stroke-primary/60" />],
        ["Warm", <Swatch className="fill-none stroke-primary" dash />],
        ["Starting", <Swatch className="fill-none stroke-muted-foreground" />],
        ["Jobs waiting", <Swatch className="fill-warning/25 stroke-warning" />],
        ["CPU", <LineSwatch className="stroke-primary" />],
        ["Memory", <LineSwatch className="stroke-muted-foreground" dash />],
      ]
    : [
        [capacity === null ? "Busy runner-minutes" : "Busy slot time", <Swatch className="fill-primary stroke-primary" />],
        ["Succeeded", <Swatch className="fill-success/60 stroke-success/60" />],
        ["Failed", <Swatch className="fill-destructive/70 stroke-destructive/70" />],
        ["Most jobs waiting", <LineSwatch className="stroke-warning" />],
        ["Average CPU", <LineSwatch className="stroke-primary" />],
      ]
  return (
    <ul aria-label="Legend" className="mb-2 flex flex-wrap gap-x-4 gap-y-1 text-xs text-muted-foreground">
      {items.map(([label, swatch]) => (
        <li key={label} className="flex items-center gap-1.5">
          {swatch}
          {label}
        </li>
      ))}
    </ul>
  )
}

function ActivityTable({ activity, now }: { activity: Activity; now: number }) {
  const lanes = isLaneWindow(activity.window)
  const rows = lanes
    ? activity.lanes.flatMap((lane, i) => lane.runs.map((run) => ({ key: run.instance_id, cells: [`Lane ${i + 1}`, runAria(run, now)] })))
    : activity.buckets.map((b) => ({ key: b.start, cells: [bucketAria(b, activity)] }))
  const headers = lanes ? ["Lane", "Run"] : ["Bucket"]
  return (
    <table className="sr-only">
      <caption>Activity data</caption>
      <thead>
        <tr>
          {headers.map((h) => (
            <th key={h} scope="col">
              {h}
            </th>
          ))}
        </tr>
      </thead>
      <tbody>
        {rows.map((r) => (
          <tr key={r.key}>
            {r.cells.map((c, i) => (
              <td key={i}>{c}</td>
            ))}
          </tr>
        ))}
      </tbody>
    </table>
  )
}

export function ActivityPanel({
  selected,
  onSelect,
  activity,
  error,
  onRetry,
  now,
  onOpenRunner,
}: {
  selected: ActivityWindow
  onSelect: (w: ActivityWindow) => void
  activity: Activity | undefined
  error: unknown
  onRetry: () => void
  now: number
  onOpenRunner: (id: string) => void
}) {
  const [ref, width] = useWidth<HTMLDivElement>(960)
  const failed = error !== null && error !== undefined
  const lanes = activity !== undefined && isLaneWindow(activity.window)
  return (
    <section aria-labelledby="activity-title" className="rounded-[10px] border border-border bg-card p-4">
      <div className="mb-3 flex flex-wrap items-center gap-x-3 gap-y-2">
        <h2 id="activity-title" className="text-base font-semibold">
          Activity
        </h2>
        {activity && <span className="font-mono text-sm text-muted-foreground">{rangeText(activity.window, activity.from, activity.to)}</span>}
        <div className="ml-auto">
          <WindowControl value={selected} onChange={onSelect} />
        </div>
      </div>
      {failed && (
        <div role="alert" className="mb-3 flex flex-wrap items-center gap-2 text-sm text-destructive">
          <span>{`Couldn't load activity: ${errorText(error)}`}</span>
          <Button size="sm" variant="outline" onClick={onRetry}>
            Retry
          </Button>
        </div>
      )}
      {activity && <Legend lanes={lanes} capacity={activity.capacity} />}
      <div ref={ref} className="overflow-x-auto">
        {activity ? (
          lanes ? (
            <LanesChart activity={activity} now={now} width={width} onOpenRunner={onOpenRunner} />
          ) : (
            <BucketsChart activity={activity} now={now} width={width} />
          )
        ) : (
          !failed && <p className="py-8 text-center text-sm text-muted-foreground">Loading activity</p>
        )}
      </div>
      {activity && (
        <>
          <p className="sr-only">{activitySummary(activity)}</p>
          <ActivityTable activity={activity} now={now} />
        </>
      )}
    </section>
  )
}
