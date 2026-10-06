export const DAY_MS = 86_400_000

const UNIT_MS: Record<string, number> = { ns: 1e-6, us: 1e-3, "µs": 1e-3, "μs": 1e-3, ms: 1, s: 1000, m: 60_000, h: 3_600_000 }
const PART = /^(\d+\.?\d*|\.\d+)(ns|us|µs|μs|ms|s|m|h)/

// Mirrors config.ParseDuration: a whole number of days ("30d"), or Go's
// time.ParseDuration syntax.
export function parseDuration(text: string): number | null {
  if (text.endsWith("d")) {
    const days = text.slice(0, -1)
    return /^[+-]?\d+$/.test(days) ? Number(days) * DAY_MS : null
  }
  let rest = text
  let sign = 1
  if (rest.startsWith("-") || rest.startsWith("+")) {
    if (rest.startsWith("-")) sign = -1
    rest = rest.slice(1)
  }
  if (rest === "0") return 0
  if (rest === "") return null
  let total = 0
  while (rest !== "") {
    const m = PART.exec(rest)
    if (!m) return null
    total += Number(m[1]) * (UNIT_MS[m[2] ?? ""] ?? 0)
    rest = rest.slice(m[0].length)
  }
  return sign * total
}

function floorText(ms: number): string {
  return ms % DAY_MS === 0 ? `${ms / DAY_MS}d` : `${ms / 1000}s`
}

export function durationError(name: string, text: string, floorMs: number): string {
  const s = text.trim()
  const v = parseDuration(s)
  if (v === null) return `invalid duration "${s}"`
  if (v <= 0) return `${name} must be greater than 0`
  if (v < floorMs) return `${name} must be at least ${floorText(floorMs)}`
  return ""
}

export function sameDuration(a: string, b: string): boolean {
  const x = parseDuration(a.trim())
  const y = parseDuration(b.trim())
  return x !== null && y !== null ? x === y : a.trim() === b.trim()
}
