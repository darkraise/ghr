import { humanBytes } from "@/lib/format"

// The bar compares a row with the largest row in view, not with the disk.
export function SizeBar({ bytes, longest }: { bytes: number; longest: number }) {
  return (
    <span className="flex items-center justify-end gap-2">
      <span className="font-mono">{humanBytes(bytes)}</span>
      <span aria-hidden="true" className="h-1.5 w-20 rounded-[2px] bg-muted">
        <span className="block h-1.5 rounded-[2px] bg-primary" style={{ width: `${Math.round((bytes / Math.max(1, longest)) * 100)}%` }} />
      </span>
    </span>
  )
}
