import type { RunDetail, RunJob } from "@/api/types"
import { Section } from "@/components/page/section"
import type { JobLinkFn } from "@/components/run-frame"
import { RunStatusIcon } from "@/components/run-status-icon"
import { dur } from "@/lib/format"
import { isRunActive } from "@/lib/run-details"

const MINUTE = 60_000

function jobEnd(job: RunJob, now: number): number | undefined {
  if (!job.started_at) return undefined
  return job.completed_at ? Date.parse(job.completed_at) : now
}

// Started jobs by start time, then the ones that never started, each group in
// the jobs API's order.
function byStart(jobs: RunJob[]): RunJob[] {
  const started = jobs.filter((j) => j.started_at)
  const rest = jobs.filter((j) => !j.started_at)
  return [...started.sort((a, b) => Date.parse(a.started_at ?? "") - Date.parse(b.started_at ?? "")), ...rest]
}

export function JobsTimeline({ run, now, jobLink }: { run: RunDetail; now: number; jobLink: JobLinkFn }) {
  const from = Date.parse(run.started_at)
  const ends = run.jobs.flatMap((j) => {
    const e = jobEnd(j, now)
    return e === undefined ? [] : [e]
  })
  const to = Math.max(from + MINUTE, isRunActive(run.status) ? now : from, ...ends)
  const span = to - from
  const ticks = Array.from({ length: Math.floor(span / MINUTE) + 1 }, (_, i) => i)
  const pct = (t: number) => `${(Math.max(0, t - from) / span) * 100}%`

  return (
    <Section title="Jobs timeline">
      {run.graph_note && <p className="-mt-2 mb-3 text-sm text-muted-foreground">{run.graph_note}</p>}
      <div className="grid grid-cols-[minmax(8rem,14rem)_minmax(0,1fr)_4.5rem] items-center gap-x-3">
        <span />
        <div aria-hidden="true" className="relative h-5 font-mono text-xs text-muted-foreground">
          {ticks.map((m) => (
            <span key={m} className="absolute -translate-x-1/2" style={{ left: `${((m * MINUTE) / span) * 100}%` }}>
              {m}m
            </span>
          ))}
        </div>
        <span />
      </div>
      <ul className="flex flex-col">
        {byStart(run.jobs).map((job) => {
          const end = jobEnd(job, now)
          const start = job.started_at ? Date.parse(job.started_at) : undefined
          return (
            <li
              key={job.id}
              className="grid grid-cols-[minmax(8rem,14rem)_minmax(0,1fr)_4.5rem] items-center gap-x-3 border-b border-border py-1.5 text-sm last:border-b-0"
            >
              {jobLink(
                job.id,
                <>
                  <RunStatusIcon status={job.status} conclusion={job.conclusion} />
                  <span className="min-w-0 truncate">{job.name}</span>
                </>,
                { className: "flex min-w-0 items-center gap-2 hover:underline" },
              )}
              {start !== undefined && end !== undefined ? (
                <>
                  <span aria-hidden="true" className="relative h-2 rounded-[2px] bg-muted">
                    <span
                      data-bar="true"
                      className={`absolute inset-y-0 rounded-[2px] ${isRunActive(job.status) ? "bg-primary" : "bg-primary/60"}`}
                      style={{ left: pct(start), width: `max(2px, ${((end - Math.max(start, from)) / span) * 100}%)` }}
                    />
                  </span>
                  <span className="text-right font-mono text-xs text-muted-foreground">{dur(end - start)}</span>
                </>
              ) : (
                <>
                  <span className="text-xs text-muted-foreground">{job.conclusion === "skipped" ? "skipped" : "queued"}</span>
                  <span />
                </>
              )}
            </li>
          )
        })}
      </ul>
    </Section>
  )
}
