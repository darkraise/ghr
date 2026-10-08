import type { Config, LastPrune, PruneScope, Status, Storage } from "@/api/types"
import { hhmm, humanBytes } from "./format"

export function storageSummary(status: Status, highWater: number): string {
  if (status.disk_total_bytes === 0) return "Not measured yet"
  const where = status.disk_root ? ` of ${status.disk_root}` : ""
  return `Docker uses ${status.disk_pct}%${where} and prunes above ${highWater}%`
}

export interface PruneAsk {
  title: string
  body: string
  action: string
  destructive: boolean
}

export interface PruneRow extends PruneAsk {
  scope: Exclude<PruneScope, "standard">
  text: string
}

const KEEP = "the build cache keep size"

function reclaimable(storage: Storage, type: string): number {
  return (storage.docker.rows ?? []).find((r) => r.type === type)?.reclaimable ?? 0
}

// Docker reports what a prune can free only for some types; a row without a
// figure says what goes and leaves the size out.
export function pruneRows(storage: Storage, config: Config | undefined): PruneRow[] {
  const keep = config?.build_cache_keep ?? KEEP
  const cache = reclaimable(storage, "Build Cache")
  const volumes = reclaimable(storage, "Local Volumes")
  const upTo = (n: number) => (n > 0 ? `, up to ${humanBytes(n)}` : "")
  const frees = (n: number) => (n > 0 ? `Frees up to ${humanBytes(n)}. ` : "")
  return [
    {
      scope: "build-cache-keep",
      text: `Build cache beyond ${keep}`,
      action: "Trim build cache",
      destructive: false,
      title: `Trim the build cache to ${keep}?`,
      body: "Build cache beyond that size is removed.",
    },
    {
      scope: "build-cache-all",
      text: `All build cache${upTo(cache)}`,
      action: "Remove build cache",
      destructive: true,
      title: "Remove all build cache?",
      body: `${frees(cache)}The next builds start cold.`,
    },
    {
      scope: "dangling-images",
      text: "Dangling images",
      action: "Remove dangling images",
      destructive: false,
      title: "Remove dangling images?",
      body: "Images that are untagged and used by no container are removed.",
    },
    {
      scope: "unused-volumes",
      text: `Unused volumes${upTo(volumes)}`,
      action: "Remove unused volumes",
      destructive: true,
      title: "Remove unused volumes?",
      body: `${frees(volumes)}Every volume no container uses is removed. It is refused while jobs run.`,
    },
  ]
}

export function standardPrune(config: Config | undefined): PruneAsk {
  const keep = config?.build_cache_keep ?? KEEP
  return {
    title: "Prune now?",
    body: `Removes build cache beyond ${keep}, dangling images, and history and logs past retention.`,
    action: "Prune",
    destructive: false,
  }
}

export function lastPruneText(p: LastPrune): string {
  const what = p.trigger === "auto" ? "auto" : `${p.trigger} ${p.scope}`
  return `Last prune: ${what} at ${hhmm(new Date(p.finished_at ?? p.started_at))}, ${p.outcome ?? "unknown"}`
}
