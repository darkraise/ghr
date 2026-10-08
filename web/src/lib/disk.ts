import type { Storage } from "@/api/types"

export interface DiskPart {
  key: string
  label: string
  bytes: number
}

const sum = (items: { bytes: number }[] | null) => (items ?? []).reduce((n, i) => n + i.bytes, 0)

export function diskParts(storage: Storage | undefined, used: number): DiskPart[] {
  if (!storage) return [{ key: "used", label: "Used", bytes: used }]
  const docker = (type: string) => storage.docker.rows?.find((r) => r.type === type)?.bytes ?? 0
  const parts: DiskPart[] = [
    { key: "build-cache", label: "Build cache", bytes: docker("Build Cache") },
    { key: "images", label: "Images", bytes: docker("Images") },
    { key: "containers", label: "Containers", bytes: docker("Containers") },
    { key: "volumes", label: "Local volumes", bytes: docker("Local Volumes") },
    { key: "package-caches", label: "Package caches", bytes: sum(storage.package_caches) },
    { key: "toolchains", label: "Toolchains", bytes: sum(storage.toolchains) + sum(storage.other_tool_cache) },
  ]
  const known = parts.reduce((n, p) => n + p.bytes, 0)
  return [...parts, { key: "other", label: "Other", bytes: Math.max(0, used - known) }]
}
