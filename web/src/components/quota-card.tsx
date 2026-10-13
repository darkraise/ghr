import type { GitHubQuota, GitHubStorage } from "@/api/types"
import { ErrorLine } from "@/components/page/error-line"
import { Section } from "@/components/page/section"
import { shortWhen } from "@/lib/format"
import { gbh, monthName, utcDay } from "@/lib/quota"
import { useNow } from "@/lib/use-now"

const TINT: Record<string, string> = {
  exhausted: "bg-destructive",
  critical: "bg-destructive",
  warn: "bg-warning",
  projected: "bg-warning",
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
  const over = q?.over_allowance_gb_hours
  const aside = q && <span className="ml-auto font-mono text-sm text-muted-foreground">{`Billing read ${shortWhen(q.read_at, now)}`}</span>
  return (
    <Section id="quota" title="Quota" aside={aside}>
      <div className="flex flex-col gap-3">
        {storage.quota_error && <ErrorLine>{storage.quota_error}</ErrorLine>}
        {q ? (
          <>
            <p className="flex flex-wrap items-baseline gap-x-2">
              <span className="font-mono text-2xl font-semibold">{`${Math.round((q.used_gb_hours / q.included_gb_hours) * 100)}%`}</span>
              <span className="text-sm text-muted-foreground">{`${gbh(q.used_gb_hours)} of ${gbh(q.included_gb_hours)} used in ${monthName(q.month)}`}</span>
            </p>
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
            <p className="text-sm">{`Actions ${gbh(q.actions_gb_hours)}, Packages ${gbh(q.packages_gb_hours)}`}</p>
            {over !== undefined && over > 0.001 && (
              <p className="text-sm text-destructive">{`GitHub reports use past its free allowance: ${over < 0.1 ? "under 0.1 GB-h" : gbh(over)}.`}</p>
            )}
            <p className="text-sm font-semibold">{outlook(q)}</p>
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
