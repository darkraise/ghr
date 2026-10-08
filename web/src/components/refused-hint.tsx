import { TriangleAlert } from "lucide-react"
import type { Status } from "@/api/types"
import { plural } from "@/lib/format"

function busyJobs(status: Status | undefined): number {
  return status?.instances.filter((i) => i.state === "busy").length ?? 0
}

export function RefusedHint({ what, status }: { what: string; status: Status | undefined }) {
  const n = busyJobs(status)
  if (n === 0) return null
  return (
    <p className="flex items-center gap-1.5 text-sm text-muted-foreground">
      <TriangleAlert size={15} aria-hidden="true" className="shrink-0 text-warning" />
      {`${what}: refused while ${plural(n, "job")} ${n === 1 ? "runs" : "run"}`}
    </p>
  )
}
