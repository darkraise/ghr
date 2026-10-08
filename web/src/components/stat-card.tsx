import type { ReactNode } from "react"
import type { Metrics, Status } from "@/api/types"
import { CapacitySegments } from "@/components/capacity-bar"
import { Sparkline } from "@/components/sparkline"
import { capacity } from "@/lib/capacity"
import { fmtMem, series } from "@/lib/format"

export function StatCard({ label, value, unit, children }: { label: string; value?: ReactNode; unit?: string; children?: ReactNode }) {
  return (
    <section aria-label={label} className="flex min-w-0 flex-col gap-1 rounded-[10px] border border-border bg-card p-3">
      <h2 className="text-sm text-muted-foreground">{label}</h2>
      <p className="flex flex-wrap items-baseline gap-x-1.5">
        {value !== undefined && <span className="font-mono text-2xl font-medium">{value}</span>}
        {unit && <span className="text-sm text-muted-foreground">{unit}</span>}
      </p>
      {children}
    </section>
  )
}

// The age moves in poll_interval steps: oldest_queued_at is only as fresh as
// the scheduler's last successful poll.
function oldestWaiting(status: Status, now: number): string | undefined {
  const times = status.repos.flatMap((r) => (r.oldest_queued_at ? [Date.parse(r.oldest_queued_at)] : []))
  if (times.length === 0) return undefined
  const minutes = Math.floor((now - Math.min(...times)) / 60_000)
  return minutes < 1 ? "oldest under 1 min" : `oldest ${minutes} min`
}

const count = (n: number) => n.toLocaleString("en-US")

export function StatCards({ status, metrics, now }: { status: Status; metrics: Metrics | undefined; now: number }) {
  const c = capacity(status)
  const samples = metrics?.samples ?? []
  const queued = status.repos.reduce((n, r) => n + r.queued, 0)
  const limit = status.rate_limit
  const cpu = metrics?.cpu
  const memory =
    metrics?.mem_used !== undefined && metrics.mem_total !== undefined ? `, ${fmtMem(metrics.mem_used)} of ${fmtMem(metrics.mem_total)} memory` : ""
  return (
    <div className="grid grid-cols-2 gap-3 min-[641px]:grid-cols-4">
      <StatCard label="Runners" value={c.busy} unit={status.mode === "all" ? "busy" : `of ${status.global_max} busy`}>
        <div className="mt-2">
          <CapacitySegments status={status} size="lg" />
        </div>
      </StatCard>
      <StatCard label="Waiting jobs" value={queued} unit={oldestWaiting(status, now)}>
        <Sparkline label="Waiting jobs over the last hour" values={series(samples, (s) => s.queued)} step className="text-warning" />
      </StatCard>
      <StatCard label="API budget" value={limit ? count(status.rate_remaining) : undefined} unit={limit ? `of ${count(limit)}` : "Not measured yet"}>
        {limit ? (
          <div
            role="meter"
            aria-label="Remaining API budget"
            aria-valuemin={0}
            aria-valuemax={limit}
            aria-valuenow={status.rate_remaining}
            className="mt-2 h-2 overflow-hidden rounded-[2px] bg-muted"
          >
            <div className="h-full bg-primary" style={{ width: `${Math.min(100, (status.rate_remaining / limit) * 100)}%` }} />
          </div>
        ) : null}
      </StatCard>
      <StatCard label="Host" value={cpu === undefined ? undefined : `${cpu.toFixed(0)}%`} unit={cpu === undefined ? "Not measured yet" : `CPU${memory}`}>
        <Sparkline label="CPU over the last hour" values={series(samples, (s) => s.cpu)} max={100} />
      </StatCard>
    </div>
  )
}
