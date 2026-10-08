import type { Status, Storage } from "@/api/types"
import { diskParts } from "@/lib/disk"
import { humanBytes } from "@/lib/format"

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

export function DiskBreakdown({ status, storage, highWater }: { status: Status; storage: Storage | undefined; highWater: number }) {
  const total = status.disk_total_bytes
  if (total === 0) return <p className="text-sm text-muted-foreground">Not measured yet</p>
  const used = status.disk_used_bytes
  const parts = diskParts(storage, used)
  return (
    <div className="flex flex-col gap-3">
      <p className="flex flex-wrap items-baseline gap-x-2">
        <span className="font-mono text-2xl font-medium">{status.disk_pct}%</span>
        <span className="text-sm text-muted-foreground">{`${humanBytes(used)} of ${humanBytes(total)} used, prunes at ${highWater}%`}</span>
      </p>
      <div className="relative">
        <div
          role="img"
          aria-label={`Disk use: ${parts.map((p) => `${p.label} ${humanBytes(p.bytes)}`).join(", ")}`}
          className="flex h-3 overflow-hidden rounded-[3px] bg-muted"
        >
          {parts.map((p) => (
            <span key={p.key} data-part={p.key} className={TINT[p.key] ?? "bg-muted-foreground/25"} style={{ width: `${(p.bytes / total) * 100}%` }} />
          ))}
        </div>
        <span data-marker="high-water" aria-hidden="true" className="absolute -top-1 h-5 w-0.5 bg-warning" style={{ left: `${highWater}%` }} />
      </div>
      <ul className="grid grid-cols-2 gap-x-4 gap-y-1 text-sm">
        {parts.map((p) => (
          <li key={p.key} className="flex items-center gap-2">
            <span aria-hidden="true" className={`size-2.5 shrink-0 rounded-[2px] ${TINT[p.key] ?? "bg-muted-foreground/25"}`} />
            <span className="flex-1">{p.label}</span>
            <span className="font-mono text-muted-foreground">{humanBytes(p.bytes)}</span>
          </li>
        ))}
      </ul>
    </div>
  )
}
