import type { StorageAlert } from "@/api/types"

export const CLEANUP_WARNING =
  "Cleanup does not protect an artifact that a different run downloads later, such as one a workflow_run workflow, a download by run ID or a nightly job reading the last build needs."

export const RETENTION_WARNING = "GitHub's retention also limits how long it keeps runs and logs, which ghr's Actions and log pages read."

export function gbh(n: number): string {
  const v = n >= 10 ? String(Math.round(n)) : n.toFixed(1).replace(/\.0$/, "")
  return `${v} GB-h`
}

// GitHub's month is taken to be the calendar month in UTC, so its dates are
// written in UTC whatever the browser's zone.
export function utcDay(iso: string): string {
  return new Date(iso).toLocaleString("en-US", { month: "short", day: "numeric", timeZone: "UTC" })
}

export function monthName(month: string): string {
  return new Date(`${month}-01T00:00:00Z`).toLocaleString("en-US", { month: "long", timeZone: "UTC" })
}

export function bannerText(alert: StorageAlert): string {
  const resets = utcDay(alert.resets_at)
  const runsOut = alert.runs_out_at ? utcDay(alert.runs_out_at) : undefined
  switch (alert.level) {
    case "exhausted":
      return `GitHub artifact storage for ${monthName(alert.month)} is used up: uploads fail until ${resets}.`
    case "critical":
    case "warn":
      return `GitHub artifact storage is at ${alert.used_pct}% of ${monthName(alert.month)}'s allowance${runsOut ? ` and runs out around ${runsOut}` : ""}.`
    case "projected":
      return `GitHub artifact storage runs out ${runsOut ? `around ${runsOut}, ` : ""}before the month resets on ${resets}.`
    default:
      return ""
  }
}

const dismissKey = (alert: StorageAlert) => `ghr-quota-dismissed:${alert.month}:${alert.level}`

// Local storage can be missing or refuse access; the banner then shows.
export function isDismissed(alert: StorageAlert): boolean {
  try {
    return localStorage.getItem(dismissKey(alert)) === "1"
  } catch {
    return false
  }
}

export function dismiss(alert: StorageAlert): void {
  try {
    localStorage.setItem(dismissKey(alert), "1")
  } catch {
    // the banner comes back on the next render, which is the safe side
  }
}
