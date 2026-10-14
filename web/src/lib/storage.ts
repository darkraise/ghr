import type { Config, LastPrune, PruneScope, Status, Storage } from "@/api/types"
import { humanBytes, shortWhen } from "./format"

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

// The button shows label; action is its accessible name and the confirm
// button's text, and starts with label so speech input can match it.
export interface PruneAction extends PruneAsk {
  scope: Exclude<PruneScope, "standard">
  type: string
  label: string
}

const KEEP = "the build cache keep size"

function reclaimable(storage: Storage, type: string): number {
  return (storage.docker.rows ?? []).find((r) => r.type === type)?.reclaimable ?? 0
}

// Docker reports what a prune can free only for some types; a confirmation
// without a figure says what goes and leaves the size out.
export function pruneActions(storage: Storage, config: Config | undefined): PruneAction[] {
  const keep = config?.build_cache_keep ?? KEEP
  const frees = (n: number) => (n > 0 ? `Frees up to ${humanBytes(n)}. ` : "")
  return [
    {
      scope: "build-cache-keep",
      type: "Build Cache",
      label: `Trim to ${keep}`,
      action: `Trim to ${keep}`,
      destructive: false,
      title: `Trim the build cache to ${keep}?`,
      body: "Build cache beyond that size is removed.",
    },
    {
      scope: "build-cache-all",
      type: "Build Cache",
      label: "Remove all",
      action: "Remove all build cache",
      destructive: true,
      title: "Remove all build cache?",
      body: `${frees(reclaimable(storage, "Build Cache"))}The next builds start cold.`,
    },
    {
      scope: "dangling-images",
      type: "Images",
      label: "Remove dangling",
      action: "Remove dangling images",
      destructive: false,
      title: "Remove dangling images?",
      body: "Images that are untagged and used by no container are removed.",
    },
    {
      scope: "unused-volumes",
      type: "Local Volumes",
      label: "Remove unused",
      action: "Remove unused volumes",
      destructive: true,
      title: "Remove unused volumes?",
      body: `${frees(reclaimable(storage, "Local Volumes"))}Every volume no container uses is removed. It is refused while a job runs or a runner starts, and idle runners stop while it runs.`,
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

export function lastPruneText(p: LastPrune, now: number): string {
  const what = p.trigger === "auto" ? "auto" : `${p.trigger} ${p.scope}`
  const freed = (p.steps ?? []).reduce((n, s) => n + s.freed, 0)
  const tail = freed > 0 ? `, ${humanBytes(freed)} freed` : ""
  return `Last prune: ${what} at ${shortWhen(p.finished_at ?? p.started_at, now)}, ${p.outcome ?? "unknown"}${tail}`
}
