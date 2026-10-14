import { TriangleAlert } from "lucide-react"
import type { Status } from "@/api/types"
import { plural } from "@/lib/format"
import { busyJobs, startingRunners } from "@/lib/status"

export function RefusedHint({ what, status }: { what: string; status: Status | undefined }) {
  const jobs = busyJobs(status)
  const starting = startingRunners(status)
  if (jobs === 0 && starting === 0) return null
  const why: string[] = []
  if (jobs > 0) why.push(`${plural(jobs, "job")} ${jobs === 1 ? "runs" : "run"}`)
  if (starting > 0) why.push(`${plural(starting, "runner")} ${starting === 1 ? "starts" : "start"}`)
  return (
    <p className="flex items-center gap-1.5 text-sm text-muted-foreground">
      <TriangleAlert size={15} aria-hidden="true" className="shrink-0 text-warning" />
      {`${what}: refused while ${why.join(" and ")}`}
    </p>
  )
}
