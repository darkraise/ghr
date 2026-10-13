import type { StoragePreview } from "@/api/types"
import { humanBytes, plural } from "@/lib/format"

// What a cleanup age would delete now, for the confirmation it gets before it
// is saved, globally or for one repository.
export function CleanupPreview({ age, previews }: { age: string; previews: StoragePreview[] }) {
  const count = previews.reduce((n, p) => n + p.count, 0)
  const bytes = previews.reduce((n, p) => n + p.bytes, 0)
  if (count === 0) return <p className="text-sm">{`No artifact is older than ${age} now.`}</p>
  return (
    <div className="flex flex-col gap-1 text-sm">
      <p>{`Up to ${plural(count, "artifact")} (${humanBytes(bytes)}) older than ${age} would go at the next check.`}</p>
      <ul className="flex flex-col gap-0.5">
        {previews.map((p) => (
          <li key={p.repo}>{`${p.repo}: up to ${plural(p.count, "artifact")}, ${humanBytes(p.bytes)}`}</li>
        ))}
      </ul>
    </div>
  )
}
