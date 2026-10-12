import type { TokenStatus } from "@/api/types"
import { DAY_MS } from "./duration"
import { count, hhmm, monthDay, plural } from "./format"

const SOON = 14 * DAY_MS

export const TOKEN_PERMISSIONS =
  "Administration: read/write and Actions: read/write on every repository ghr serves; Contents: read is optional and draws job graphs."

export function tokenWord(t: TokenStatus, now: number): string {
  if (t.state !== "ok") return t.state
  if (!t.expires_at) return "valid"
  // The daemon's last check can still say ok after the expiry has passed.
  const left = Date.parse(t.expires_at) - now
  if (left <= 0) return "expired"
  return left < SOON ? "expires soon" : "valid"
}

export function expiryText(t: TokenStatus, now: number): string {
  if (!t.expires_at) return "Expiry unknown"
  const at = Date.parse(t.expires_at)
  const day = monthDay(new Date(at))
  const left = at - now
  if (left <= 0) return `Expired ${day}`
  return left < DAY_MS ? `Expires ${day}, in less than a day` : `Expires ${day}, in ${plural(Math.floor(left / DAY_MS), "day")}`
}

export function rateText(t: TokenStatus | undefined): string {
  if (t?.rate_remaining === undefined) return ""
  const limit = t.rate_limit !== undefined ? ` of ${count(t.rate_limit)}` : ""
  const reset = t.rate_reset ? `, resets ${hhmm(new Date(t.rate_reset))}` : ""
  return `${count(t.rate_remaining)}${limit}${reset}`
}
