import type { InstanceStatus, MetricSample } from "@/api/types"

const pad = (n: number) => String(n).padStart(2, "0")

export function dur(ms: number): string {
  const total = Math.max(0, Math.round(ms / 1000))
  const h = Math.floor(total / 3600)
  const m = Math.floor(total / 60) % 60
  const s = total % 60
  return h > 0 ? `${h}h${pad(m)}m` : `${m}m${pad(s)}s`
}

export function ago(ms: number): string {
  const minutes = ms / 60_000
  if (minutes < 1) return "just now"
  if (minutes < 60) return `${Math.floor(minutes)}m ago`
  const hours = minutes / 60
  if (hours < 48) return `${Math.floor(hours)}h ago`
  return `${Math.floor(hours / 24)}d ago`
}

const UNITS = ["B", "kB", "MB", "GB", "TB", "PB"]

export function humanBytes(n: number): string {
  let f = n
  let i = 0
  while (f >= 1000 && i < UNITS.length - 1) {
    f /= 1000
    i++
  }
  if (i === 0) return `${n} B`
  let s = f.toFixed(1)
  if (s === "1000.0" && i < UNITS.length - 1) {
    f /= 1000
    i++
    s = f.toFixed(1)
  }
  return `${s} ${UNITS[i]}`
}

export function fmtMem(bytes: number): string {
  return bytes >= 2 ** 30 ? `${(bytes / 2 ** 30).toFixed(1)}G` : `${Math.floor(bytes / 2 ** 20)}M`
}

export function maxText(n: number): string {
  return n === 0 ? "∞" : String(n)
}

export function isZeroTime(iso: string | undefined): boolean {
  return !iso || iso.startsWith("0001-01-01")
}

export function hhmm(d: Date): string {
  return `${pad(d.getHours())}:${pad(d.getMinutes())}`
}

export function clock(iso: string): string {
  const d = new Date(iso)
  return `${hhmm(d)}:${pad(d.getSeconds())}`
}

export function dateTime(iso: string): string {
  const d = new Date(iso)
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${hhmm(d)}`
}

export function dateTimeSec(iso: string): string {
  return `${dateTime(iso)}:${pad(new Date(iso).getSeconds())}`
}

export function startedAt(instance: InstanceStatus): string {
  const job = instance.job
  return job && !isZeroTime(job.started_at) ? job.started_at : instance.since
}

export function elapsed(instance: InstanceStatus, now: number): string {
  return dur(now - Date.parse(startedAt(instance)))
}

export function series(samples: MetricSample[], pick: (s: MetricSample) => number | undefined): (number | null)[] {
  const out: (number | null)[] = []
  let prev: number | undefined
  for (const s of samples) {
    const t = Date.parse(s.at)
    if (prev !== undefined && t - prev > 90_000) out.push(null)
    prev = t
    out.push(pick(s) ?? null)
  }
  return out
}

const MONTHS = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"]

export function monthDay(d: Date): string {
  return `${MONTHS[d.getMonth()] ?? ""} ${d.getDate()}`
}

export function plural(n: number, word: string): string {
  return `${n} ${word}${n === 1 ? "" : "s"}`
}
