import type { Activity, ActivityBucket } from "@/api/types"
import { bucketAria, bucketTickLabel, GUTTER, lineRuns, PAD_RIGHT, retentionText, scaleX, sinceText, windowWords } from "@/lib/activity-view"

const TRACK_H = 44
const TRACK_GAP = 16
const AXIS_H = 18

type Point = { x: number; y: number }
const points = (run: Point[]) => run.map((p) => `${p.x.toFixed(1)},${p.y.toFixed(1)}`).join(" ")
const known = (values: (number | null)[]) => values.flatMap((v) => (v === null ? [] : [v]))

function Line({ runs, track, stroke, fill }: { runs: Point[][]; track: string; stroke: string; fill: string }) {
  return (
    <>
      {runs.map((run, i) =>
        run.length > 1 ? (
          <polyline key={i} data-track={track} points={points(run)} strokeWidth={1.5} className={`fill-none ${stroke}`} />
        ) : (
          <circle key={i} data-track={track} cx={run[0]?.x} cy={run[0]?.y} r={2} className={fill} />
        ),
      )}
    </>
  )
}

export function BucketsChart({
  activity,
  now,
  width,
  tracks: mode = "all",
  picked = null,
  onPick,
}: {
  activity: Activity
  now: number
  width: number
  tracks?: "all" | "jobs"
  picked?: string | null
  onPick?: (bucket: ActivityBucket) => void
}) {
  const buckets = activity.buckets
  const jobs = mode === "jobs"
  const trackCount = jobs ? 2 : 4
  const plotW = width - GUTTER - PAD_RIGHT
  const colW = plotW / Math.max(1, buckets.length)
  const colX = (i: number) => GUTTER + i * colW
  const center = (i: number) => colX(i) + colW / 2
  const top = (k: number) => k * (TRACK_H + TRACK_GAP)
  const axisTop = top(trackCount - 1) + TRACK_H
  const height = axisTop + AXIS_H

  const all = activity.capacity === null
  const busy = buckets.map((b) => (all ? b.busy_minutes : Math.min(100, b.busy_pct ?? 0)))
  const busyPeak = Math.max(0, ...busy)
  const busyScale = all ? Math.max(1, busyPeak) : 100
  const runsPeak = Math.max(0, ...buckets.map((b) => b.succeeded + b.failed))
  const runsScale = Math.max(1, runsPeak)
  const waits = buckets.map((b) => b.waiting_max)
  const cpus = buckets.map((b) => b.cpu_avg)
  const waitPeak = Math.max(0, ...known(waits))
  const cpuPeak = Math.max(0, ...known(cpus))
  const waitRuns = lineRuns(
    waits.map((v, i) => ({ x: center(i), v })),
    (v) => top(2) + TRACK_H - (v / Math.max(1, waitPeak)) * TRACK_H,
  )
  const cpuRuns = lineRuns(
    cpus.map((v, i) => ({ x: center(i), v })),
    (v) => top(3) + TRACK_H - (Math.min(v, 100) / 100) * TRACK_H,
  )

  const first = buckets[0]
  const last = buckets.at(-1)
  const start = first ? Date.parse(first.start) : now
  const end = last ? Date.parse(last.end) : now
  const historyFrom = Date.parse(activity.history_from)
  const totalRuns = buckets.reduce((n, b) => n + b.succeeded + b.failed + b.cancelled, 0)
  const nowX = last
    ? colX(buckets.length - 1) + colW * Math.min(1, Math.max(0, (now - Date.parse(last.start)) / (Date.parse(last.end) - Date.parse(last.start))))
    : GUTTER + plotW

  const allTracks: [string, string][] = [
    ["Busy", all ? `peak ${Math.round(busyPeak)} min` : `peak ${Math.round(busyPeak)}%`],
    ["Runs", `peak ${runsPeak}`],
    ["Waiting", known(waits).length > 0 ? `peak ${waitPeak}` : ""],
    ["CPU", known(cpus).length > 0 ? `peak ${Math.round(cpuPeak)}%` : ""],
  ]
  const tracks = allTracks.slice(0, trackCount)

  // A null is a gap, never a zero; nulls from the start of the window mean
  // the metrics began part-way through it.
  function collecting(values: (number | null)[], k: number) {
    const lead = values.findIndex((v) => v !== null)
    if (lead === 0 || values.length === 0) return null
    const since = buckets[lead < 0 ? buckets.length - 1 : lead]
    if (!since) return null
    return (
      <text x={GUTTER + 6} y={top(k) + TRACK_H / 2 + 4} className="fill-muted-foreground">
        {sinceText(activity.window, since.start)}
      </text>
    )
  }

  return (
    <svg
      width={width}
      height={height}
      viewBox={`0 0 ${width} ${height}`}
      role="group"
      aria-label={`${jobs ? "Jobs" : "Activity"} per bucket for ${windowWords(activity.window)}`}
      className="block text-xs"
    >
      {tracks.map(([name, peak], k) => (
        <g key={name}>
          <text x={0} y={top(k) + 12} className="fill-muted-foreground">
            {name}
          </text>
          {peak && (
            <text x={0} y={top(k) + 26} className="fill-muted-foreground font-mono">
              {peak}
            </text>
          )}
          <rect x={GUTTER} y={top(k)} width={plotW} height={TRACK_H} className="fill-muted/30" />
        </g>
      ))}
      {buckets.map((b, i) => {
        const label = bucketTickLabel(activity.window, b.start)
        return label === null ? null : (
          <g key={`tick-${b.start}`}>
            <line x1={colX(i)} x2={colX(i)} y1={0} y2={axisTop} className="stroke-border" />
            <text x={colX(i) + 2} y={axisTop + 13} className="fill-muted-foreground font-mono">
              {label}
            </text>
          </g>
        )
      })}
      {historyFrom > start && (
        <g data-retention="true">
          <rect x={GUTTER} y={0} width={scaleX(historyFrom, start, end, plotW)} height={axisTop} className="fill-muted" />
          {scaleX(historyFrom, start, end, plotW) > retentionText(activity).length * 6.5 + 12 && (
            <text x={GUTTER + 6} y={12} className="fill-muted-foreground">
              {retentionText(activity)}
            </text>
          )}
        </g>
      )}
      {buckets.map((b, i) => {
        const open = i === buckets.length - 1
        const busyH = ((busy[i] ?? 0) / busyScale) * TRACK_H
        const okH = (b.succeeded / runsScale) * TRACK_H
        const failH = (b.failed / runsScale) * TRACK_H
        const x0 = colX(i) + 1
        const w = Math.max(1, colW - 2)
        const label = bucketAria(b, activity)
        return (
          <g
            key={b.start}
            tabIndex={0}
            role={onPick ? "button" : "img"}
            aria-label={label}
            aria-pressed={onPick ? picked === b.start : undefined}
            data-open={open ? "true" : undefined}
            className={`ghr-mark ${open ? "opacity-60" : ""} ${onPick ? "cursor-pointer" : ""}`}
            onClick={onPick ? () => onPick(b) : undefined}
            onKeyDown={
              onPick
                ? (e) => {
                    // A held key repeats keydown, which would toggle the pick on and off.
                    if (!e.repeat && (e.key === "Enter" || e.key === " ")) {
                      e.preventDefault()
                      onPick(b)
                    }
                  }
                : undefined
            }
          >
            <title>{label}</title>
            <rect x={colX(i)} y={0} width={colW} height={axisTop} className="ghr-mark-box fill-transparent" />
            {busyH > 0 && <rect data-kind="busy" x={x0} y={top(0) + TRACK_H - busyH} width={w} height={busyH} className="fill-primary" />}
            {okH > 0 && <rect data-kind="succeeded" x={x0} y={top(1) + TRACK_H - okH} width={w} height={okH} className="fill-success/60" />}
            {failH > 0 && (
              <rect data-kind="failed" x={x0} y={top(1) + TRACK_H - okH - failH} width={w} height={failH} className="fill-destructive/70" />
            )}
            {picked === b.start && (
              <rect data-picked="true" x={colX(i)} y={0} width={colW} height={axisTop} strokeWidth={1.5} className="fill-none stroke-primary" />
            )}
          </g>
        )
      })}
      {!jobs && (
        <>
          <Line runs={waitRuns} track="waiting" stroke="stroke-warning" fill="fill-warning" />
          <Line runs={cpuRuns} track="cpu" stroke="stroke-primary" fill="fill-primary" />
          {collecting(waits, 2)}
          {collecting(cpus, 3)}
        </>
      )}
      {totalRuns === 0 && (
        <text x={GUTTER + plotW / 2} y={top(1) + TRACK_H / 2 + 4} textAnchor="middle" className="fill-muted-foreground">
          {`No jobs ran in ${windowWords(activity.window)}.`}
        </text>
      )}
      <line data-now="true" x1={nowX} x2={nowX} y1={0} y2={axisTop} strokeWidth={1.5} className="stroke-primary" />
    </svg>
  )
}
