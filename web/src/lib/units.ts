import { DAY_MS, parseDuration } from "@/lib/duration"

// A setting the daemon holds as text ("2m0s", "6G", "200%") that the form
// edits as a number in one fixed unit. read returns null for text the unit
// cannot show, such as a memory_max of "50%" set in config.yaml.
export interface UnitSetting {
  unit: string
  // What a screen reader says for unit, when unit itself reads badly aloud.
  spoken?: string
  read: (raw: string) => number | null
  write: (n: number) => string
  error: (n: number) => string
}

const BINARY: Record<string, number> = { "": 1, B: 1, K: 1024, KB: 1024, M: 1024 ** 2, MB: 1024 ** 2, G: 1024 ** 3, GB: 1024 ** 3, T: 1024 ** 4, TB: 1024 ** 4 }

function bytesIn(raw: string, units: RegExp): number | null {
  const m = units.exec(raw.trim())
  return m ? Number(m[1]) * (BINARY[m[2] ?? ""] ?? Number.NaN) : null
}

function whole(n: number | null): number | null {
  return n !== null && Number.isInteger(n) ? n : null
}

function seconds(floor: number): UnitSetting {
  return {
    unit: "s",
    spoken: "seconds",
    read: (raw) => {
      const ms = parseDuration(raw.trim())
      return ms === null ? null : ms / 1000
    },
    write: (n) => `${n}s`,
    error: (n) => {
      if (Number.isNaN(n)) return "Enter a number of seconds"
      if (floor > 0 && n < floor) return `Enter at least ${floor} seconds`
      return n > 0 ? "" : "Enter more than 0 seconds"
    },
  }
}

function hours(floor: number): UnitSetting {
  return {
    unit: "h",
    spoken: "hours",
    read: (raw) => {
      const ms = parseDuration(raw.trim())
      return ms === null ? null : ms / 3_600_000
    },
    write: (n) => `${n}h`,
    error: (n) => (Number.isNaN(n) ? "Enter a number of hours" : n < floor ? `Enter at least ${floor} hour${floor === 1 ? "" : "s"}` : ""),
  }
}

function minutes(floor: number): UnitSetting {
  return {
    unit: "min",
    spoken: "minutes",
    read: (raw) => {
      const ms = parseDuration(raw.trim())
      return ms === null ? null : ms / 60_000
    },
    write: (n) => `${n}m`,
    error: (n) => (Number.isNaN(n) ? "Enter a number of minutes" : n < floor ? `Enter at least ${floor} minutes` : ""),
  }
}

// config.ParseDuration takes only whole days, so a fraction goes as seconds.
const days: UnitSetting = {
  unit: "days",
  read: (raw) => {
    const ms = parseDuration(raw.trim())
    return ms === null ? null : ms / DAY_MS
  },
  write: (n) => (Number.isInteger(n) ? `${n}d` : `${Math.round(n * 86_400)}s`),
  error: (n) => (Number.isNaN(n) ? "Enter a number of days" : n < 1 ? "Enter at least 1 day" : ""),
}

// An empty field is no limit: systemd's MemoryMax=infinity.
const megabytes: UnitSetting = {
  unit: "MB",
  read: (raw) => {
    if (raw.trim() === "infinity") return Number.NaN
    const b = bytesIn(raw, /^(\d+)([KMGT]?)$/)
    return whole(b === null ? null : b / 1024 ** 2)
  },
  write: (n) => (Number.isNaN(n) ? "infinity" : `${n}M`),
  error: (n) => (Number.isNaN(n) || (Number.isInteger(n) && n >= 1) ? "" : "Enter a whole number of MB, or leave it empty for no limit"),
}

const percent: UnitSetting = {
  unit: "%",
  read: (raw) => {
    const m = /^(\d+)%$/.exec(raw.trim())
    return m ? Number(m[1]) : null
  },
  write: (n) => `${n}%`,
  error: (n) => (Number.isInteger(n) && n >= 1 ? "" : "Enter a whole number of 1 or more"),
}

// Docker reads build_cache_keep with binary units, so 1TB is 1024GB.
const gigabytes: UnitSetting = {
  unit: "GB",
  read: (raw) => {
    const b = bytesIn(raw, /^(\d+)(B|KB|MB|GB|TB)$/)
    return whole(b === null ? null : b / 1024 ** 3)
  },
  write: (n) => `${n}GB`,
  error: (n) => (Number.isInteger(n) && n >= 0 ? "" : "Enter a whole number of 0 or more"),
}

export const UNIT_SETTINGS: Record<string, UnitSetting> = {
  poll_interval: seconds(5),
  start_timeout: seconds(0),
  idle_timeout: seconds(0),
  history_retention: days,
  build_cache_keep: gigabytes,
  memory_max: megabytes,
  cpu_quota: percent,
  // The daemon's floors: config.Validate refuses less.
  "jobs.ghr_update_check.every": hours(1),
  "jobs.runner_update_check.every": hours(1),
  "jobs.prune.every": hours(1),
  "jobs.storage_measure.every": minutes(5),
}

// The form holds the number, or the daemon's text when read gave null; that
// text is shown as the placeholder and sent back unchanged.
export function readSetting(key: string, raw: string): number | string {
  const spec = UNIT_SETTINGS[key]
  return spec?.read(raw) ?? raw
}

export function writeSetting(key: string, value: unknown): unknown {
  const spec = UNIT_SETTINGS[key]
  return spec && typeof value === "number" ? spec.write(value) : value
}
