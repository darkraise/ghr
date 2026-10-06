import type { HistoryEntry } from "@/api/types"
import { DAY_MS, parseDuration } from "./duration"
import { dur } from "./format"

const WINDOW_MS = 7 * DAY_MS
const STRIP = 20

export interface StripMark {
  id: string
  symbol: string
  label: string
  className: string
}

export interface Activity {
  line: string
  label: string
  strip: StripMark[]
}

const MARKS: Record<string, [string, string]> = {
  success: ["■", "text-green-600"],
  failure: ["✖", "text-destructive"],
  cancelled: ["○", "text-muted-foreground"],
}

export function activityWindow(retention: string | undefined): { ms: number; label: string } {
  const r = retention === undefined ? null : parseDuration(retention)
  return r !== null && r > 0 && r < WINDOW_MS ? { ms: r, label: `last ${retention}` } : { ms: WINDOW_MS, label: "last 7 days" }
}

// Every conclusion GitHub reports counts as known; only a missing one does not.
export function summarize(entries: HistoryEntry[], now: number, retention: string | undefined, limit: number): Activity {
  const win = activityWindow(retention)
  const recent = entries.filter((e) => Date.parse(e.finished_at) >= now - win.ms)
  let ok = 0
  let known = 0
  let total = 0
  for (const e of recent) {
    total += Date.parse(e.finished_at) - Date.parse(e.started_at)
    if (e.conclusion !== "" && e.conclusion !== "unknown") {
      known++
      if (e.conclusion === "success") ok++
    }
  }
  const rate = known > 0 ? `${Math.floor((ok * 100) / known)}%` : "–"
  const avg = recent.length > 0 ? dur(total / recent.length) : "–"
  const more = entries.length >= limit ? "≥" : ""
  const strip = recent
    .slice(0, STRIP)
    .reverse()
    .map((e) => {
      const [symbol, className] = MARKS[e.conclusion] ?? ["?", "text-muted-foreground"]
      return { id: e.id, symbol, className, label: `#${e.run_number} ${e.job_name}: ${e.conclusion || "unknown"}` }
    })
  return { line: `${more}${recent.length} jobs · ${rate} success · avg ${avg}`, label: win.label, strip }
}
