import type { HistoryEntry } from "@/api/types"
import { dur, hhmm, monthDay, plural } from "@/lib/format"

export const HISTORY_WINDOWS = ["24h", "7d", "30d"] as const
export type HistoryWindow = (typeof HISTORY_WINDOWS)[number]

export const RESULTS = [
  { value: "success", label: "Succeeded" },
  { value: "failure", label: "Failed" },
  { value: "cancelled", label: "Cancelled" },
] as const
export type HistoryResult = (typeof RESULTS)[number]["value"]

export interface HistorySearch {
  repo?: string
  result?: HistoryResult
  window?: HistoryWindow
}

// Unknown values are dropped, so the page falls back to its defaults.
export function parseHistorySearch(search: Record<string, unknown>): HistorySearch {
  const out: HistorySearch = {}
  if (typeof search.repo === "string" && search.repo !== "") out.repo = search.repo
  const result = RESULTS.find((r) => r.value === search.result)
  if (result) out.result = result.value
  const window = HISTORY_WINDOWS.find((w) => w === search.window)
  if (window) out.window = window
  return out
}

const WEEKDAYS = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"]
const dayKey = (d: Date) => `${d.getFullYear()}-${d.getMonth()}-${d.getDate()}`

export function dayLabel(iso: string, now: number): string {
  const d = new Date(iso)
  const today = new Date(now)
  const yesterday = new Date(today.getFullYear(), today.getMonth(), today.getDate() - 1)
  if (dayKey(d) === dayKey(today)) return "Today"
  if (dayKey(d) === dayKey(yesterday)) return "Yesterday"
  return `${WEEKDAYS[d.getDay()] ?? ""}, ${monthDay(d)}`
}

export interface DayGroup {
  key: string
  label: string
  rows: HistoryEntry[]
}

export function groupByDay(rows: HistoryEntry[], now: number): DayGroup[] {
  const out: DayGroup[] = []
  for (const r of rows) {
    const key = dayKey(new Date(r.finished_at))
    const last = out.at(-1)
    if (last?.key === key) last.rows.push(r)
    else out.push({ key, label: dayLabel(r.finished_at, now), rows: [r] })
  }
  return out
}

export function took(entry: HistoryEntry): number {
  return Date.parse(entry.finished_at) - Date.parse(entry.started_at)
}

export function median(values: number[]): number | undefined {
  if (values.length === 0) return undefined
  const sorted = [...values].sort((a, b) => a - b)
  const mid = Math.floor(sorted.length / 2)
  if (sorted.length % 2 === 1) return sorted[mid]
  return ((sorted[mid - 1] ?? 0) + (sorted[mid] ?? 0)) / 2
}

// As the result icons read them: anything not a success, a cancellation, a
// skip or unknown is a failure.
export function resultWord(conclusion: string): string {
  if (conclusion === "success") return "Succeeded"
  if (conclusion === "cancelled") return "Cancelled"
  if (conclusion === "skipped") return "Skipped"
  if (conclusion === "unknown") return "Unknown"
  return "Failed"
}

export function historySummary(rows: HistoryEntry[]): string {
  if (rows.length === 0) return "No jobs"
  const failed = rows.filter((r) => resultWord(r.conclusion) === "Failed").length
  const mid = median(rows.map(took)) ?? 0
  return `${plural(rows.length, "job")}${failed > 0 ? `, ${failed} failed` : ""}, median ${dur(mid)}`
}

export function inBucket(rows: HistoryEntry[], bucket: { start: string; end: string }): HistoryEntry[] {
  const start = Date.parse(bucket.start)
  const end = Date.parse(bucket.end)
  return rows.filter((r) => {
    const t = Date.parse(r.finished_at)
    return t >= start && t < end
  })
}

export function pickText(bucket: { start: string; end: string }): string {
  const start = new Date(bucket.start)
  const end = new Date(bucket.end)
  if (end.getTime() - start.getTime() >= 24 * 3_600_000) return `Showing ${monthDay(start)}`
  return `Showing ${hhmm(start)} to ${hhmm(end)}, ${monthDay(start)}`
}
