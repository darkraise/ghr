import { Spinner } from "darkraise-ui/components/spinner"
import type { ReactNode } from "react"
import { useActivity } from "@/api/hooks"
import type { RepoStatus } from "@/api/types"
import { BucketsChart } from "@/components/buckets-chart"
import { ErrorLine } from "@/components/page/error-line"
import { Section } from "@/components/page/section"
import { ResultIcon } from "@/components/result-icon"
import { RunnerMeter } from "@/components/runner-meter"
import { ago } from "@/lib/format"
import { runnersText, weekRate } from "@/lib/repos"
import { useMediaQuery, WIDEST } from "@/lib/use-media-query"
import { useWidth } from "@/lib/use-width"
import { errorText } from "@/query"

function Fact({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="flex flex-col gap-0.5">
      <dt className="text-xs text-muted-foreground">{label}</dt>
      <dd className="flex flex-wrap items-center gap-2 text-sm">{children}</dd>
    </div>
  )
}

// The chart keeps its 720px minimum, so the facts move beside it only when
// both fit.
export function RepoActivity({ name, repo, now }: { name: string; repo: RepoStatus | undefined; now: number }) {
  const activity = useActivity("24h", name)
  const wide = useMediaQuery(WIDEST)
  const [ref, width] = useWidth<HTMLDivElement>(720)
  const week = activity.data?.repos.find((r) => r.repo.toLowerCase() === name.toLowerCase())?.week
  const rate = weekRate(week)
  const job = repo?.last_job
  return (
    <Section title="Last 24 hours">
      {activity.isError && (
        <div className="mb-3">
          <ErrorLine onRetry={() => void activity.refetch()}>{errorText(activity.error)}</ErrorLine>
        </div>
      )}
      <div className={wide ? "flex items-start gap-6" : "flex flex-col gap-4"}>
        <div ref={ref} className="min-w-0 flex-1 overflow-x-auto">
          {activity.data ? (
            <BucketsChart activity={activity.data} now={now} width={width} tracks="jobs" />
          ) : (
            !activity.isError && <Spinner label="Loading" />
          )}
        </div>
        <dl className={wide ? "grid w-56 shrink-0 content-start gap-3" : "grid grid-cols-2 gap-3 sm:grid-cols-4"}>
          <Fact label="Runners">
            {repo && (
              <>
                <RunnerMeter active={repo.active} max={repo.max} />
                <span className="font-mono">{runnersText(repo)}</span>
              </>
            )}
          </Fact>
          <Fact label="Waiting">{repo && <span className="font-mono">{repo.queued}</span>}</Fact>
          <Fact label="Last 7 days">
            {week && <span>{`${week.succeeded} succeeded, ${week.failed} failed${rate === undefined ? "" : `, ${rate}%`}`}</span>}
          </Fact>
          <Fact label="Last job">
            {job ? (
              <>
                <ResultIcon conclusion={job.conclusion} />
                <span>
                  {job.job_name} <span className="font-mono">#{job.run_number}</span>
                </span>
                <span className="text-muted-foreground">{ago(now - Date.parse(job.finished_at))}</span>
              </>
            ) : (
              <span className="text-muted-foreground">None yet</span>
            )}
          </Fact>
        </dl>
      </div>
    </Section>
  )
}
