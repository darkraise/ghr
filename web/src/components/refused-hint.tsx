import type { Status } from "@/api/types"

function busyJobs(status: Status | undefined): number {
  return status?.instances.filter((i) => i.state === "busy").length ?? 0
}

export function RefusedHint({ what, status }: { what: string; status: Status | undefined }) {
  const n = busyJobs(status)
  if (n === 0) return null
  return (
    <p className="text-sm text-amber-600">
      {what}: refused while {n} jobs run
    </p>
  )
}
