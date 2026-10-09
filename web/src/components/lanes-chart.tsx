import type { Activity, ActivityRun } from "@/api/types"
import { GUTTER, hasJob, laneTicks, lineRuns, PAD_RIGHT, retentionText, runAria, runEnd, runLabel, scaleX, windowWords } from "@/lib/activity-view"
import { fmtMem } from "@/lib/format"

const LANE_H = 20
const LANE_GAP = 6
const TRACK_H = 36
const TRACK_GAP = 16
const AXIS_H = 18
const MINUTE = 60_000

const SEGMENT: Record<string, string | undefined> = {
  starting: "fill-none stroke-muted-foreground",
  warm: "fill-none stroke-primary",
  running: "fill-primary stroke-primary",
  succeeded: "fill-success/30 stroke-success/60",
  failed: "fill-destructive/30 stroke-destructive/70",
  cancelled: "fill-muted stroke-muted-foreground",
  skipped: "fill-muted stroke-muted-foreground",
  unknown: "fill-muted stroke-muted-foreground",
}

type Point = { x: number; y: number }
const points = (run: Point[]) => run.map((p) => `${p.x.toFixed(1)},${p.y.toFixed(1)}`).join(" ")

function RunMark({
  run,
  top,
  x,
  now,
  onOpenRunner,
}: {
  run: ActivityRun
  top: number
  x: (t: number) => number
  now: number
  onOpenRunner: (id: string) => void
}) {
  const label = runAria(run, now)
  const last = run.segments.at(-1)
  const first = run.segments[0]
  const startX = first ? x(Date.parse(first.from)) : 0
  const endX = x(runEnd(run, now))
  const text = runLabel(run)
  const fits = text !== "" && endX - startX > text.length * 6.5 + 8
  const body = (
    <>
      <title>{label}</title>
      {run.segments.map((s, i) => {
        const x1 = x(Date.parse(s.from))
        const x2 = x(s.to === null ? now : Date.parse(s.to))
        return (
          <rect
            key={i}
            data-state={s.state}
            x={x1}
            y={top + 0.5}
            width={Math.max(2, x2 - x1)}
            height={LANE_H - 1}
            rx={2}
            strokeWidth={1}
            strokeDasharray={s.state === "warm" || s.state === "unknown" ? "3 2" : undefined}
            className={`ghr-mark-box ${SEGMENT[s.state] ?? "fill-muted stroke-muted-foreground"}`}
          />
        )
      })}
      {last?.state === "running" && last.to === null && (
        <rect x={endX - 3} y={top + 0.5} width={3} height={LANE_H - 1} className="ghr-pulse fill-foreground/70" />
      )}
      {fits && (
        <text
          x={startX + 4}
          y={top + LANE_H / 2 + 4}
          className={`pointer-events-none ${last?.state === "running" ? "fill-primary-foreground" : "fill-foreground"}`}
        >
          {text}
        </text>
      )}
    </>
  )
  if (last?.to === null) {
    return (
      <a
        href={`/runners/${encodeURIComponent(run.instance_id)}`}
        aria-label={label}
        className="ghr-mark"
        onClick={(e) => {
          e.preventDefault()
          onOpenRunner(run.instance_id)
        }}
      >
        {body}
      </a>
    )
  }
  if (run.html_url?.startsWith("https://")) {
    return (
      <a href={run.html_url} target="_blank" rel="noreferrer" aria-label={label} className="ghr-mark">
        {body}
      </a>
    )
  }
  return (
    <g tabIndex={0} role="img" aria-label={label} className="ghr-mark">
      {body}
    </g>
  )
}

// The time axis ends at now and slides each second, so running bars grow
// between polls.
export function LanesChart({
  activity,
  now,
  width,
  onOpenRunner,
}: {
  activity: Activity
  now: number
  width: number
  onOpenRunner: (id: string) => void
}) {
  const from = now - (Date.parse(activity.to) - Date.parse(activity.from))
  const plotW = width - GUTTER - PAD_RIGHT
  const x = (t: number) => GUTTER + scaleX(t, from, now, plotW)
  const laneCount = Math.max(1, activity.lanes.length)
  const laneTop = (i: number) => i * (LANE_H + LANE_GAP)
  const lanesH = laneTop(laneCount) - LANE_GAP
  const waitTop = lanesH + TRACK_GAP
  const cpuTop = waitTop + TRACK_H + TRACK_GAP
  const axisTop = cpuTop + TRACK_H
  const height = axisTop + AXIS_H
  const historyFrom = Date.parse(activity.history_from)
  const jobCount = activity.lanes.reduce((n, l) => n + l.runs.filter(hasJob).length, 0)

  const waiting = activity.waiting.map((p) => ({ t: Date.parse(p.at), v: p.value }))
  const waitPeak = Math.max(0, ...waiting.map((p) => p.v))
  const waitY = (v: number) => waitTop + TRACK_H - (v / Math.max(1, waitPeak)) * TRACK_H
  const waitFirst = waiting[0]
  let waitPath = ""
  if (waitFirst) {
    const base = waitTop + TRACK_H
    waitPath = `M ${x(waitFirst.t)} ${base}`
    waiting.forEach((p, i) => {
      const next = waiting[i + 1]
      waitPath += ` V ${waitY(p.v)} H ${x(next ? next.t : Math.min(now, p.t + MINUTE))}`
    })
    waitPath += ` V ${base} Z`
  }

  const samples = activity.cpu.map((p) => ({ x: x(Date.parse(p.at)), cpu: p.cpu, mem: p.mem }))
  const cpuPeak = Math.max(0, ...samples.flatMap((p) => (p.cpu === null ? [] : [p.cpu])))
  const memPeak = Math.max(1, ...samples.flatMap((p) => (p.mem === null ? [] : [p.mem])))
  const cpuLines = lineRuns(
    samples.map((p) => ({ x: p.x, v: p.cpu })),
    (v) => cpuTop + TRACK_H - (Math.min(v, 100) / 100) * TRACK_H,
  )
  const memLines = lineRuns(
    samples.map((p) => ({ x: p.x, v: p.mem })),
    (v) => cpuTop + TRACK_H - (v / memPeak) * TRACK_H,
  )

  return (
    <svg
      width={width}
      height={height}
      viewBox={`0 0 ${width} ${height}`}
      role="group"
      aria-label={`Runner lanes for ${windowWords(activity.window)}`}
      className="block text-xs"
    >
      {Array.from({ length: laneCount }, (_, i) => (
        <g key={i}>
          <text x={0} y={laneTop(i) + LANE_H / 2 + 4} className="fill-muted-foreground font-mono">
            {`Lane ${i + 1}`}
          </text>
          <rect x={GUTTER} y={laneTop(i)} width={plotW} height={LANE_H} rx={3} className="fill-muted/50" />
        </g>
      ))}
      {laneTicks(from, now, activity.window).map((t) => (
        <g key={t.at}>
          <line x1={x(t.at)} x2={x(t.at)} y1={0} y2={axisTop} className="stroke-border" />
          <text x={x(t.at)} y={axisTop + 13} textAnchor="middle" className="fill-muted-foreground font-mono">
            {t.label}
          </text>
        </g>
      ))}
      {historyFrom > from && (
        <g data-retention="true">
          <rect x={GUTTER} y={0} width={x(historyFrom) - GUTTER} height={axisTop} className="fill-muted" />
          {x(historyFrom) - GUTTER > retentionText(activity).length * 6.5 + 12 && (
            <text x={GUTTER + 6} y={12} className="fill-muted-foreground">
              {retentionText(activity)}
            </text>
          )}
        </g>
      )}
      {activity.lanes.map((lane, i) =>
        lane.runs.map((run) => <RunMark key={run.instance_id} run={run} top={laneTop(i)} x={x} now={now} onOpenRunner={onOpenRunner} />),
      )}
      {jobCount === 0 && (
        <text x={GUTTER + plotW / 2} y={lanesH / 2 + 4} textAnchor="middle" className="fill-muted-foreground">
          {`No jobs ran in ${windowWords(activity.window)}.`}
        </text>
      )}

      <text x={0} y={waitTop + 12} className="fill-muted-foreground">
        Waiting
      </text>
      {waitFirst && (
        <text x={0} y={waitTop + 26} className="fill-muted-foreground font-mono">
          {`peak ${waitPeak}`}
        </text>
      )}
      <rect x={GUTTER} y={waitTop} width={plotW} height={TRACK_H} className="fill-muted/30" />
      {waitPath && <path data-track="waiting" d={waitPath} strokeWidth={1} className="fill-warning/25 stroke-warning" />}

      <text x={0} y={cpuTop + 12} className="fill-muted-foreground">
        CPU
      </text>
      {cpuLines.length > 0 && (
        <text x={0} y={cpuTop + 26} className="fill-muted-foreground font-mono">
          {`peak ${Math.round(cpuPeak)}%`}
        </text>
      )}
      {memLines.length > 0 && (
        <text x={0} y={cpuTop + TRACK_H} className="fill-muted-foreground font-mono">
          {`mem ${fmtMem(memPeak)}`}
        </text>
      )}
      <rect x={GUTTER} y={cpuTop} width={plotW} height={TRACK_H} className="fill-muted/30" />
      {cpuLines.map((run, i) =>
        run.length > 1 ? (
          <polyline key={i} data-track="cpu" points={points(run)} strokeWidth={1.5} className="fill-none stroke-primary" />
        ) : (
          <circle key={i} data-track="cpu" cx={run[0]?.x} cy={run[0]?.y} r={1.5} className="fill-primary" />
        ),
      )}
      {memLines.map((run, i) =>
        run.length > 1 ? (
          <polyline key={i} data-track="mem" points={points(run)} strokeWidth={1} strokeDasharray="3 2" className="fill-none stroke-muted-foreground" />
        ) : (
          <circle key={i} data-track="mem" cx={run[0]?.x} cy={run[0]?.y} r={1.5} className="fill-muted-foreground" />
        ),
      )}

      <line data-now="true" x1={GUTTER + plotW} x2={GUTTER + plotW} y1={0} y2={axisTop} strokeWidth={1.5} className="stroke-primary" />
    </svg>
  )
}
