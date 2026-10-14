import type { Storage } from "@/api/types"

export interface DiskPart {
  key: string
  label: string
  bytes: number
  docker?: string
}

// Accent and grey tints only: a category is not a status.
const TINT: Record<string, string | undefined> = {
  "build-cache": "bg-primary",
  images: "bg-primary/70",
  containers: "bg-primary/45",
  volumes: "bg-primary/25",
  "package-caches": "bg-muted-foreground/70",
  toolchains: "bg-muted-foreground/45",
  other: "bg-muted-foreground/25",
  used: "bg-primary",
}

export function partTint(key: string): string {
  return TINT[key] ?? "bg-muted-foreground/25"
}

const sum = (items: { bytes: number }[] | null) => (items ?? []).reduce((n, i) => n + i.bytes, 0)

const cacheParts = (storage: Storage): DiskPart[] => [
  { key: "package-caches", label: "Package caches", bytes: sum(storage.package_caches) },
  { key: "toolchains", label: "Toolchains", bytes: sum(storage.toolchains) + sum(storage.other_tool_cache) },
]

// The caches that sit on a different filesystem than Docker's data root, and
// so are not part of the disk the bar measures.
export function otherFilesystemParts(storage: Storage | undefined): DiskPart[] {
  return storage && !storage.caches_on_data_root ? cacheParts(storage) : []
}

export function diskParts(storage: Storage | undefined, used: number): DiskPart[] {
  if (!storage) return [{ key: "used", label: "Used", bytes: used }]
  const docker = (key: string, label: string, type: string): DiskPart => ({
    key,
    docker: type,
    label,
    bytes: storage.docker.rows?.find((r) => r.type === type)?.bytes ?? 0,
  })
  const parts: DiskPart[] = [
    docker("build-cache", "Build cache", "Build Cache"),
    docker("images", "Images", "Images"),
    docker("containers", "Containers", "Containers"),
    docker("volumes", "Local volumes", "Local Volumes"),
    ...(storage.caches_on_data_root ? cacheParts(storage) : []),
  ]
  const known = parts.reduce((n, p) => n + p.bytes, 0)
  return [...parts, { key: "other", label: "Other", bytes: Math.max(0, used - known) }]
}
