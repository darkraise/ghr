import type { Activity, ActivityBucket, ActivityRun, ActivityWindow } from "@/api/types"
import { dur, hhmm, monthDay, plural } from "./format"

export const ACTIVITY_WINDOWS: readonly ActivityWindow[] = ["1h", "3h", "24h", "7d", "30d"]

// Room on the left of every chart for lane and track labels.
export const GUTTER = 64
export const PAD_RIGHT = 8

const HOUR = 3_600_000
const JOB_STATES = new Set(["running", "succeeded", "failed", "cancelled", "skipped"])

const WORDS: Record<string, string | undefined> = {
  "1h": "the last hour",
  "3h": "the last 3 hours",
  "24h": "the last 24 hours",
  "7d": "the last 7 days",
  "30d": "the last 30 days",
}

const RESULT: Record<string, string | undefined> = {
  starting: "starting",
  warm: "warm, waiting for a job",
  running: "running",
  succeeded: "succeeded",
  failed: "failed",
  cancelled: "cancelled",
  skipped: "skipped",
}

export function hasJob(run: ActivityRun): boolean {
  return run.segments.some((s) => JOB_STATES.has(s.state))
}

export function isLaneWindow(w: string): boolean {
  return w === "1h" || w === "3h"
}

export function windowWords(w: string): string {
  return WORDS[w] ?? `the last ${w}`
}

export function rangeText(window: string, from: string, to: string): string {
  const f = new Date(from)
  const t = new Date(to)
  if (isLaneWindow(window)) return `${hhmm(f)} to ${hhmm(t)}`
  if (window === "24h") return `${monthDay(f)} ${hhmm(f)} to ${monthDay(t)} ${hhmm(t)}, per hour`
  return `${monthDay(f)} to ${monthDay(t)}, ${window === "7d" ? "per 6 hours" : "per day"}`
}

export function scaleX(t: number, from: number, to: number, width: number): number {
  if (to <= from) return 0
  return Math.min(width, Math.max(0, ((t - from) / (to - from)) * width))
}

export function laneTicks(from: number, to: number, window: string): { at: number; label: string }[] {
  const step = window === "3h" ? 30 : 10
  const first = new Date(from)
  first.setSeconds(0, 0)
  first.setMinutes(Math.ceil(first.getMinutes() / step) * step)
  const ticks: { at: number; label: string }[] = []
  for (let t = first.getTime(); t <= to; t += step * 60_000) {
    if (t >= from) ticks.push({ at: t, label: hhmm(new Date(t)) })
  }
  return ticks
}

export function bucketTickLabel(window: string, start: string): string | null {
  const d = new Date(start)
  if (window === "24h") return d.getHours() % 6 === 0 ? hhmm(d) : null
  if (window === "7d") return d.getHours() === 0 ? monthDay(d) : null
  if (window === "30d") return d.getDay() === 1 ? monthDay(d) : null
  return null
}

export function runEnd(run: ActivityRun, now: number): number {
  const to = run.segments.at(-1)?.to
  return to === undefined || to === null ? now : Date.parse(to)
}

export function runLabel(run: ActivityRun): string {
  if (!run.job) return ""
  return [run.repo, run.job, run.run_number && `#${run.run_number}`].filter(Boolean).join(" ")
}

export function runAria(run: ActivityRun, now: number): string {
  const last = run.segments.at(-1)
  const job = [...run.segments].reverse().find((s) => JOB_STATES.has(s.state)) ?? last
  const ms = job ? (job.to === null ? now : Date.parse(job.to)) - Date.parse(job.from) : 0
  const who = run.job ? [run.repo, run.workflow, run.job, run.run_number && `run ${run.run_number}`] : [run.repo, `runner ${run.instance_id}`]
  const state = last ? (RESULT[last.state] ?? last.state) : ""
  return [...who, state, dur(ms)].filter(Boolean).join(", ")
}

function bucketRange(window: string, b: ActivityBucket): string {
  const s = new Date(b.start)
  const e = new Date(b.end)
  if (window === "24h") return `${hhmm(s)} to ${hhmm(e)}`
  if (window === "7d") return `${monthDay(s)} ${hhmm(s)} to ${hhmm(e)}`
  return monthDay(s)
}

// busy_pct divides by today's global_max, not the cap in force then.
export function bucketAria(b: ActivityBucket, a: { window: string; capacity: number | null }): string {
  const busy =
    a.capacity === null ? `${Math.round(b.busy_minutes)} busy runner-minutes` : `${b.busy_pct ?? 0}% busy against today's max of ${a.capacity}`
  return [
    bucketRange(a.window, b),
    busy,
    `${b.succeeded} succeeded`,
    `${b.failed} failed`,
    `${b.cancelled} cancelled`,
    b.waiting_max === null ? "waiting not measured" : `at most ${b.waiting_max} waiting`,
    b.cpu_avg === null ? "CPU not measured" : `CPU ${Math.round(b.cpu_avg)}%`,
  ].join(", ")
}

export function activitySummary(a: Activity): string {
  const words = windowWords(a.window)
  if (isLaneWindow(a.window)) {
    const jobs = a.lanes.flatMap((l) => l.runs).filter(hasJob)
    if (jobs.length === 0) return `No jobs ran in ${words}.`
    const failed = jobs.filter((r) => r.segments.at(-1)?.state === "failed").length
    const running = jobs.filter((r) => r.segments.some((s) => s.state === "running" && s.to === null)).length
    return `${plural(jobs.length, "job")} in ${words}, ${failed} failed, ${running} running.`
  }
  const total = a.buckets.reduce((n, b) => n + b.succeeded + b.failed + b.cancelled, 0)
  if (total === 0) return `No jobs ran in ${words}.`
  const failed = a.buckets.reduce((n, b) => n + b.failed, 0)
  return `${plural(total, "job")} in ${words}, ${failed} failed.`
}

// Pruning runs once a day, so the edge this describes is a guide.
export function retentionText(a: Activity): string {
  const hours = Math.round((Date.parse(a.to) - Date.parse(a.history_from)) / HOUR)
  return hours % 24 === 0 ? `History is kept for ${plural(hours / 24, "day")}` : `History is kept for ${plural(hours, "hour")}`
}

export function sinceText(window: string, iso: string): string {
  const d = new Date(iso)
  return `Collecting data since ${window === "24h" ? hhmm(d) : `${monthDay(d)} ${hhmm(d)}`}`
}

export function lineRuns(points: { x: number; v: number | null }[], y: (v: number) => number): { x: number; y: number }[][] {
  const runs: { x: number; y: number }[][] = []
  let current: { x: number; y: number }[] = []
  for (const p of points) {
    if (p.v === null) {
      if (current.length > 0) runs.push(current)
      current = []
      continue
    }
    current.push({ x: p.x, y: y(p.v) })
  }
  if (current.length > 0) runs.push(current)
  return runs
}
