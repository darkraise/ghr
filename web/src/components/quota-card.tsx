import type { GitHubQuota, GitHubStorage } from "@/api/types"
import { ErrorLine } from "@/components/page/error-line"
import { Section } from "@/components/page/section"
import { StatCard } from "@/components/stat-card"
import { shortWhen } from "@/lib/format"
import { gbh, utcDay } from "@/lib/quota"
import { useNow } from "@/lib/use-now"

const TINT: Record<string, string> = {
  exhausted: "bg-destructive",
  critical: "bg-destructive",
  warn: "bg-warning",
  projected: "bg-warning",
}

const VALUE_TINT: Record<string, string> = {
  exhausted: "text-destructive",
  critical: "text-destructive",
  warn: "text-warning",
  projected: "text-warning",
}

// The Dashboard's tile grid, with a rule below in place of its frame.
const TILES =
  "grid grid-cols-2 divide-border border-b border-border [&>*:nth-child(-n+2)]:border-b [&>*:nth-child(odd)]:border-r min-[641px]:grid-cols-4 min-[641px]:divide-x min-[641px]:[&>*:nth-child(-n+2)]:border-b-0"

// Calendar days in UTC. A kept quota whose billing read now fails can be from
// last month, so a reset that is not ahead has no count to show.
function untilReset(iso: string, now: number): string | undefined {
  const n = Math.floor(Date.parse(iso) / 86_400_000) - Math.floor(now / 86_400_000)
  if (n < 1) return undefined
  return n === 1 ? "in 1 day" : `in ${n} days`
}

// A share of the allowance as a bar position, capped at the bar's end.
const at = (n: number, of: number) => `${Math.min(100, Math.round((n / of) * 1000) / 10)}%`

function outlook(q: GitHubQuota): string {
  if (q.level === "exhausted") return `Uploads fail until ${utcDay(q.resets_at)} unless a budget is set.`
  if (q.runs_out_at) return `Runs out around ${utcDay(q.runs_out_at)}.`
  if (q.estimated_gb_hours >= q.included_gb_hours) return "Uploads may fail before GitHub's next recalculation."
  return `Lasts until the month resets on ${utcDay(q.resets_at)}.`
}

function QuotaMeter({ q }: { q: GitHubQuota }) {
  const label = `Used ${gbh(q.used_gb_hours)} of ${gbh(q.included_gb_hours)}, estimated ${gbh(q.estimated_gb_hours)}, projected ${gbh(q.projected_gb_hours)} by ${utcDay(q.resets_at)}`
  return (
    <div role="img" aria-label={label} className="relative">
      <div className="flex h-3 overflow-hidden rounded-[3px] bg-muted">
        <span data-part="used" className={TINT[q.level] ?? "bg-primary"} style={{ width: at(q.used_gb_hours, q.included_gb_hours) }} />
      </div>
      <span data-marker="estimate" aria-hidden="true" className="absolute -top-1 h-5 w-0.5 bg-foreground" style={{ left: at(q.estimated_gb_hours, q.included_gb_hours) }} />
      <span data-marker="projected" aria-hidden="true" className="absolute -top-1 h-5 w-0.5 bg-warning" style={{ left: at(q.projected_gb_hours, q.included_gb_hours) }} />
    </div>
  )
}

export function QuotaCard({ storage }: { storage: GitHubStorage }) {
  const now = useNow()
  const q = storage.quota
  const aside = q && <span className="ml-auto font-mono text-sm text-muted-foreground">{`Billing read ${shortWhen(q.read_at, now)}`}</span>
  return (
    <Section id="quota" title="Quota" aside={aside} flush>
      {storage.quota_error && (
        <div className="border-b border-border p-4">
          <ErrorLine>{storage.quota_error}</ErrorLine>
        </div>
      )}
      {q && (
        <div className={TILES}>
          <StatCard
            label="Used"
            value={<span className={VALUE_TINT[q.level] ?? ""}>{`${Math.round((q.used_gb_hours / q.included_gb_hours) * 100)}%`}</span>}
            unit={`${gbh(q.used_gb_hours)} of ${gbh(q.included_gb_hours)}`}
          />
          <StatCard label="Estimated now" value={gbh(q.estimated_gb_hours)} />
          <StatCard label={`Projected by ${utcDay(q.resets_at)}`} value={gbh(q.projected_gb_hours)} />
          <StatCard label="Resets" value={utcDay(q.resets_at)} unit={untilReset(q.resets_at, now)} />
        </div>
      )}
      <div className="flex flex-col gap-3 p-4">
        {q ? (
          <>
            <QuotaMeter q={q} />
            <ul className="flex flex-col gap-0.5 text-sm">
              <li className="flex items-center gap-2">
                <span aria-hidden="true" className="h-3 w-0.5 bg-foreground" />
                {`Estimated now ${gbh(q.estimated_gb_hours)}`}
              </li>
              <li className="flex items-center gap-2">
                <span aria-hidden="true" className="h-3 w-0.5 bg-warning" />
                {`Projected by ${utcDay(q.resets_at)} ${gbh(q.projected_gb_hours)}`}
              </li>
            </ul>
            <p className="text-sm font-semibold">{outlook(q)}</p>
            <p className="text-sm">{`Actions ${gbh(q.actions_gb_hours)}, Packages ${gbh(q.packages_gb_hours)}`}</p>
          </>
        ) : (
          !storage.quota_error && <p className="text-sm text-muted-foreground">No billing read yet.</p>
        )}
        <p className="text-xs text-muted-foreground">
          GitHub recalculates storage every 6 to 12 hours, so the estimate adds 12 hours at the current rate. The month is taken to be the calendar month in UTC.
        </p>
      </div>
    </Section>
  )
}
