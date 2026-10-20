import type { ArtifactLinkTTL, ArtifactStore, ArtifactStoreItem, ArtifactStoreRepo, Config, Deletion } from "@/api/types"
import { parseDuration } from "./duration"
import { dateTime, repositories, shortWhen } from "./format"

export interface ArtifactsSearch {
  store?: "self-hosted"
}

// Any other value is dropped, so the page falls back to the GitHub tab.
export function parseArtifactsSearch(search: Record<string, unknown>): ArtifactsSearch {
  return search.store === "self-hosted" ? { store: "self-hosted" } : {}
}

export const LINK_TTLS: readonly { ttl: ArtifactLinkTTL; label: string }[] = [
  { ttl: "1h", label: "1 hour" },
  { ttl: "24h", label: "24 hours" },
  { ttl: "7d", label: "7 days" },
]

export type StateTone = "ok" | "warn" | "error" | "off"

export const STATE_TONE: Record<StateTone, string> = {
  ok: "text-success",
  warn: "text-warning",
  error: "text-destructive",
  off: "text-muted-foreground",
}

export function stateLine(r: ArtifactStoreRepo): { text: string; tone: StateTone } {
  if (!r.private || r.state === "public") return { text: "Public repositories stay on GitHub", tone: "off" }
  switch (r.state) {
    case "set":
      return { text: "Variable and secret set · uploads go to RustFS", tone: "ok" }
    case "missing":
      return { text: "Variable and secret not written yet · the next check writes them", tone: "warn" }
    case "stale":
      return { text: "The secret holds an old job key · the next check rewrites it", tone: "warn" }
    case "foreign":
      return { text: r.state_error || "Another ghr set this repository's variable; ghr leaves it alone", tone: "warn" }
    case "error":
      return { text: r.state_error || "Could not set the variable and secret", tone: "error" }
    default:
      return { text: "Off · uploads go to GitHub", tone: "off" }
  }
}

export function storeName(store: string | undefined): string {
  return store === "rustfs" ? "RustFS" : "GitHub"
}

// A GitHub deletion has no store field; naming it keeps the merged list's rows
// telling the two apart.
export function mergeDeletions(github: Deletion[], selfHosted: Deletion[]): Deletion[] {
  const all = [...github.map((d) => ({ ...d, store: d.store || "github" })), ...selfHosted.map((d) => ({ ...d, store: d.store || "rustfs" }))]
  return all.sort((a, b) => Date.parse(b.at) - Date.parse(a.at))
}

// null when the age cannot be read or is not positive.
export function olderThan(items: ArtifactStoreItem[], age: string, now: number): ArtifactStoreItem[] | null {
  const ms = parseDuration(age.trim())
  if (ms === null || ms <= 0) return null
  return items.filter((i) => now - Date.parse(i.uploaded_at) > ms)
}

// An item without its own expiry goes once it is older than the repository's
// cleanup age; the day shown is when that age is reached.
export function expiryText(item: ArtifactStoreItem, cleanup: string): string {
  if (item.expires_at) return dateTime(item.expires_at).slice(0, 10)
  const ms = cleanup === "off" ? null : parseDuration(cleanup)
  if (ms === null || ms <= 0) return "Kept"
  return dateTime(new Date(Date.parse(item.uploaded_at) + ms).toISOString()).slice(0, 10)
}

export function globalCleanup(config: Config | undefined): string {
  const age = config?.artifact_store?.cleanup_max_age ?? ""
  return age === "" ? "7d" : age
}

export function selfHostedSummary(store: ArtifactStore | undefined, config: Config | undefined, now: number): string | undefined {
  if (!store) return undefined
  if (!store.configured) return "Not set up"
  const checked = store.checked_at ? `Checked ${shortWhen(store.checked_at, now)}` : "Not checked yet"
  const n = store.repos.filter((r) => r.cleanup_source === "override").length
  const overrides = n === 0 ? "" : ` (${n} repository override${n === 1 ? "" : "s"})`
  const on = store.repos.filter((r) => r.enabled).length
  return `${checked} · cleanup ${globalCleanup(config)}${overrides} · ${repositories(on)} on`
}
