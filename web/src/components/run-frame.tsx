import { Button } from "darkraise-ui/components/button"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "darkraise-ui/components/select"
import { PageHeader } from "darkraise-ui/layout"
import type { ReactNode } from "react"
import type { RunDetail, RunJob } from "@/api/types"
import { RunStatusIcon } from "@/components/run-status-icon"
import { dur } from "@/lib/format"
import { attemptLabel, githubLinks, jobMs, runDescription } from "@/lib/run-details"

export interface JobLinkOptions {
  step?: number
  line?: number
  className?: string
  label?: string
  current?: boolean
}

// The pages pass typed router links; tests pass plain anchors, so the frame
// and the sections render without a router.
export type JobLinkFn = (jobId: number, children: ReactNode, opts?: JobLinkOptions) => ReactNode
export type SummaryLinkFn = (children: ReactNode, opts: { className?: string; current?: boolean }) => ReactNode

// The router marks the current link with aria-current, so the style follows it.
const RAIL_LINK =
  "flex items-center gap-2 rounded-md px-2 py-1.5 text-sm hover:bg-muted aria-[current=page]:bg-muted aria-[current=page]:font-medium"
const RAIL_LABEL = "mt-3 px-2 pb-1 text-xs font-medium text-muted-foreground"

export function RunFrame({
  run,
  owner,
  now,
  current,
  job,
  jobLink,
  summaryLink,
  onAttempt,
  children,
}: {
  run: RunDetail
  owner: string
  now: number
  current: "summary" | number
  job?: RunJob
  jobLink: JobLinkFn
  summaryLink: SummaryLinkFn
  onAttempt: (attempt: number) => void
  children: ReactNode
}) {
  const runPath = `/actions/${encodeURIComponent(run.repo)}/runs/${run.id}`
  const breadcrumbs = [
    { label: "Actions", href: "/actions" },
    { label: run.repo, href: `/actions?repo=${encodeURIComponent(run.repo)}` },
    { label: run.workflow },
    job ? { label: `#${run.run_number}`, href: `${runPath}?attempt=${run.attempt}` } : { label: `#${run.run_number}` },
    ...(job ? [{ label: job.name }] : []),
  ]
  const attempts = Array.from({ length: run.attempts }, (_, i) => run.attempts - i)
  const workflowFile = githubLinks(owner, run).workflowFile
  const actions = (
    <div className="flex flex-wrap items-center gap-2">
      {run.attempts > 1 && (
        <Select value={String(run.attempt)} onValueChange={(v) => onAttempt(Number(v))}>
          <SelectTrigger aria-label="Attempt" className="w-48">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {attempts.map((n) => (
              <SelectItem key={n} value={String(n)}>
                {attemptLabel(n, run.attempts)}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      )}
      <Button variant="outline" asChild>
        <a href={run.html_url} target="_blank" rel="noreferrer">
          Open on GitHub
        </a>
      </Button>
    </div>
  )

  return (
    <div className="flex flex-col gap-4">
      <PageHeader breadcrumbs={breadcrumbs} title={run.title} description={runDescription(run)} actions={actions} />
      <div className="grid gap-4 min-[800px]:grid-cols-[15rem_minmax(0,1fr)] min-[800px]:items-start">
        <nav aria-label="Run" className="flex flex-col min-[800px]:sticky min-[800px]:top-4">
          {summaryLink("Summary", { className: RAIL_LINK, current: current === "summary" })}
          <p className={RAIL_LABEL}>Jobs</p>
          <ul className="flex flex-col">
            {run.jobs.map((j) => {
              const ms = jobMs(j, now)
              return (
                <li key={j.id}>
                  {jobLink(
                    j.id,
                    <>
                      <RunStatusIcon status={j.status} conclusion={j.conclusion} />
                      <span className="min-w-0 flex-1 break-words">{j.name}</span>
                      {ms !== undefined && <span className="font-mono text-xs text-muted-foreground">{dur(ms)}</span>}
                    </>,
                    { className: RAIL_LINK, current: current === j.id },
                  )}
                </li>
              )
            })}
          </ul>
          <p className={RAIL_LABEL}>Run details</p>
          {workflowFile && (
            <a href={workflowFile} target="_blank" rel="noreferrer" className={RAIL_LINK}>
              Workflow file
              <span className="sr-only">, opens in a new tab</span>
            </a>
          )}
        </nav>
        <div className="flex min-w-0 flex-col gap-4">{children}</div>
      </div>
    </div>
  )
}
