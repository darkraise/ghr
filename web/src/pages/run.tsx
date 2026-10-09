import { useMutation } from "@tanstack/react-query"
import { Link, SearchParamError, useNavigate, useParams, useSearch, type ErrorComponentProps } from "@tanstack/react-router"
import { Button } from "darkraise-ui/components/button"
import { Spinner } from "darkraise-ui/components/spinner"
import { PageHeader } from "darkraise-ui/layout"
import { useId, type ReactNode } from "react"
import { ApiError, api } from "@/api/client"
import { useConfig, useJobLog, useRunAnnotations, useRunDetail } from "@/api/hooks"
import type { RunDetail, RunJob } from "@/api/types"
import { LogViewer } from "@/components/log-viewer"
import { ErrorLine } from "@/components/page/error-line"
import { Section } from "@/components/page/section"
import { RouteError } from "@/components/route-error"
import { AnnotationsSection } from "@/components/run-annotations"
import { ArtifactsSection } from "@/components/run-artifacts"
import { RunFrame, type JobLinkFn, type SummaryLinkFn } from "@/components/run-frame"
import { JobGraph } from "@/components/run-graph"
import { RunStatusIcon } from "@/components/run-status-icon"
import { JobsTimeline } from "@/components/run-timeline"
import { StepList } from "@/components/step-list"
import { dateTimeSec, dur } from "@/lib/format"
import { githubLinks, isRunActive, jobMs, positiveInt, rawLogFileName, runTotalMs, statusText, type JobSearch } from "@/lib/run-details"
import { useNow } from "@/lib/use-now"
import { errorText } from "@/query"

const GONE = "This run no longer exists on GitHub."

function runError(err: unknown): string {
  return err instanceof ApiError && err.status === 404 ? GONE : errorText(err)
}

function Bare({ repo, children }: { repo: string; children: ReactNode }) {
  return (
    <div className="flex flex-col gap-4">
      <PageHeader breadcrumbs={[{ label: "Actions", href: "/actions" }, { label: repo }]} title="Workflow run" />
      {children}
    </div>
  )
}

// The routes' error view: their search parser refuses a bad attempt, step or
// line. Any other error is the app's usual one.
export function RunNotFound(props: ErrorComponentProps) {
  const params = useParams({ strict: false })
  if (!(props.error instanceof SearchParamError)) return <RouteError {...props} />
  return (
    <Bare repo={params.repo ?? ""}>
      <ErrorLine>{GONE}</ErrorLine>
    </Bare>
  )
}

function RunGate({
  repo,
  idParam,
  attempt,
  invalid = false,
  children,
}: {
  repo: string
  idParam: string
  attempt: number | undefined
  invalid?: boolean
  children: (run: RunDetail, owner: string) => ReactNode
}) {
  const config = useConfig()
  const id = positiveInt(idParam)
  const valid = id !== undefined && !invalid
  const names = [...(config.data?.repos ?? []).map((r) => r.name), ...(config.data?.watch_repos ?? [])]
  const covered = names.some((n) => n.toLowerCase() === repo.toLowerCase())
  const run = useRunDetail(repo, id ?? 0, attempt, valid && covered)

  if (!valid) {
    return (
      <Bare repo={repo}>
        <ErrorLine>{GONE}</ErrorLine>
      </Bare>
    )
  }
  if (!config.data) {
    return (
      <Bare repo={repo}>
        {config.isError ? <ErrorLine onRetry={() => void config.refetch()}>{errorText(config.error)}</ErrorLine> : <Spinner label="Loading" />}
      </Bare>
    )
  }
  if (!covered) {
    return (
      <Bare repo={repo}>
        <div className="flex flex-col items-start gap-2">
          <ErrorLine>{`${repo} is not configured or watched in ghr.`}</ErrorLine>
          <Link to="/repositories" className="text-sm text-primary hover:underline">
            Open Repositories
          </Link>
        </div>
      </Bare>
    )
  }
  if (!run.data) {
    return (
      <Bare repo={repo}>
        {run.isError ? (
          <ErrorLine onRetry={() => void run.refetch()}>{runError(run.error)}</ErrorLine>
        ) : (
          <Spinner label="Loading" />
        )}
      </Bare>
    )
  }
  return (
    <>
      {run.isError && <ErrorLine onRetry={() => void run.refetch()}>{runError(run.error)}</ErrorLine>}
      {children(run.data, config.data.owner)}
    </>
  )
}

function linksFor(run: RunDetail): { jobLink: JobLinkFn; summaryLink: SummaryLinkFn } {
  const params = { repo: run.repo, id: String(run.id) }
  return {
    jobLink: (jobId, children, opts = {}) => (
      <Link
        to="/actions/$repo/runs/$id/jobs/$jobId"
        params={{ ...params, jobId: String(jobId) }}
        search={{ attempt: run.attempt, step: opts.step, line: opts.line }}
        activeOptions={{ includeSearch: false }}
        className={opts.className}
        aria-label={opts.label}
      >
        {children}
      </Link>
    ),
    // exact: a job page's path starts with the run page's, and would
    // otherwise mark Summary as the current page too.
    summaryLink: (children, opts) => (
      <Link
        to="/actions/$repo/runs/$id"
        params={params}
        search={{ attempt: run.attempt }}
        activeOptions={{ exact: true, includeSearch: false }}
        className={opts.className}
      >
        {children}
      </Link>
    ),
  }
}

function useAttemptNavigate(run: RunDetail): (attempt: number) => void {
  const navigate = useNavigate()
  return (attempt) => void navigate({ to: "/actions/$repo/runs/$id", params: { repo: run.repo, id: String(run.id) }, search: { attempt } })
}

function Fact({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="flex min-w-0 flex-col gap-1">
      <dt className="text-xs text-muted-foreground">{label}</dt>
      <dd className="flex flex-wrap items-center gap-x-2 gap-y-0.5 text-sm">{children}</dd>
    </div>
  )
}

function SummaryFacts({ run, owner, now }: { run: RunDetail; owner: string; now: number }) {
  const links = githubLinks(owner, run)
  return (
    <dl className="grid grid-cols-2 gap-4 sm:grid-cols-4">
      <Fact label={run.event ? `Triggered via ${run.event}` : "Triggered"}>
        {run.actor && <span>{run.actor}</span>}
        <a href={links.commit} target="_blank" rel="noreferrer" className="font-mono text-primary hover:underline">
          {run.head_sha.slice(0, 7)}
        </a>
        {run.pull_requests.map((n) => (
          <a key={n} href={links.pull(n)} target="_blank" rel="noreferrer" className="font-mono text-primary hover:underline">
            #{n}
          </a>
        ))}
      </Fact>
      <Fact label="Status">
        <RunStatusIcon status={run.status} conclusion={run.conclusion} />
        <span>{statusText(run.status, run.conclusion)}</span>
      </Fact>
      <Fact label="Total duration">
        <span className="font-mono">{dur(runTotalMs(run, now))}</span>
      </Fact>
      <Fact label="Artifacts">
        <span className="font-mono">{run.artifacts.length}</span>
      </Fact>
    </dl>
  )
}

function RunSummary({ run, owner }: { run: RunDetail; owner: string }) {
  const now = useNow()
  const onAttempt = useAttemptNavigate(run)
  const annotations = useRunAnnotations(run.repo, run.id, run.attempt, isRunActive(run.status), true)
  const { jobLink, summaryLink } = linksFor(run)
  return (
    <RunFrame run={run} owner={owner} now={now} current="summary" jobLink={jobLink} summaryLink={summaryLink} onAttempt={onAttempt}>
      <Section title="Summary">
        <SummaryFacts run={run} owner={owner} now={now} />
      </Section>
      {run.graph ? (
        <JobGraph graph={run.graph} jobs={run.jobs} path={run.path} event={run.event} now={now} jobLink={jobLink} />
      ) : (
        <JobsTimeline run={run} now={now} jobLink={jobLink} />
      )}
      <AnnotationsSection data={annotations.data} error={annotations.error} onRetry={() => void annotations.refetch()} jobLink={jobLink} />
      <ArtifactsSection repo={run.repo} artifacts={run.artifacts} />
    </RunFrame>
  )
}

export function RunPage() {
  const params = useParams({ from: "/app/actions/$repo/runs/$id" })
  const search = useSearch({ from: "/app/actions/$repo/runs/$id" })
  return (
    <RunGate repo={params.repo} idParam={params.id} attempt={search.attempt}>
      {(run, owner) => <RunSummary run={run} owner={owner} />}
    </RunGate>
  )
}

function JobMeta({ job }: { job: RunJob }) {
  return (
    <div className="mt-1 flex flex-wrap gap-x-3 gap-y-0.5 text-xs text-muted-foreground">
      {job.runner_name && <span className="font-mono">{job.runner_name}</span>}
      {job.labels.length > 0 && (
        <span className="flex flex-wrap gap-x-2 font-mono">
          {job.labels.map((l) => (
            <span key={l}>{l}</span>
          ))}
        </span>
      )}
      {job.ghr && <span className="font-mono">ghr</span>}
      {job.started_at && <span className="font-mono">{dateTimeSec(job.started_at)}</span>}
    </div>
  )
}

// The Blob goes through a temporary object URL because a navigation to the
// API cannot send X-GHR. The URL is revoked after the click has started the
// download, not during it.
function saveText(text: string, fileName: string) {
  const url = URL.createObjectURL(new Blob([text], { type: "text/plain" }))
  const a = document.createElement("a")
  a.href = url
  a.download = fileName
  document.body.appendChild(a)
  a.click()
  a.remove()
  setTimeout(() => URL.revokeObjectURL(url), 0)
}

function JobLogs({ repo, job, search }: { repo: string; job: RunJob; search: JobSearch }) {
  const log = useJobLog(repo, job.id, true)
  const raw = useMutation({
    mutationFn: () => api.rawJobLog(repo, job.id),
    onSuccess: (r) => saveText(r.text, rawLogFileName(repo, job.id)),
  })
  const rawButton = (
    <Button size="sm" variant="outline" loading={raw.isPending} onClick={() => raw.mutate()}>
      Raw log
    </Button>
  )
  if (!log.data) {
    return log.isError ? <ErrorLine onRetry={() => void log.refetch()}>{runError(log.error)}</ErrorLine> : <Spinner label="Loading" />
  }
  if (log.data.expired) return <p className="text-sm text-muted-foreground">GitHub no longer keeps this job&apos;s log.</p>
  if (log.data.too_large) {
    return (
      <div className="flex flex-col items-start gap-2">
        <p className="text-sm text-muted-foreground">This log is too large to show here.</p>
        {rawButton}
      </div>
    )
  }
  // LogViewer reads step and line once, when it mounts; a new annotation
  // link on the same job page must mount it again.
  return (
    <LogViewer
      key={`${job.id}:${search.step ?? ""}:${search.line ?? ""}`}
      log={log.data}
      steps={job.steps}
      step={search.step}
      line={search.line}
      toolbar={rawButton}
    />
  )
}

function JobView({ run, owner, jobId, search }: { run: RunDetail; owner: string; jobId: number; search: JobSearch }) {
  const now = useNow()
  const onAttempt = useAttemptNavigate(run)
  const headingId = useId()
  const { jobLink, summaryLink } = linksFor(run)
  const job = run.jobs.find((j) => j.id === jobId)
  if (!job) {
    return (
      <RunFrame run={run} owner={owner} now={now} current={jobId} jobLink={jobLink} summaryLink={summaryLink} onAttempt={onAttempt}>
        <div className="flex flex-col items-start gap-2">
          <p className="text-sm text-muted-foreground">{`This job is not part of attempt ${run.attempt}.`}</p>
          {summaryLink("Open the run", { className: "text-sm text-primary hover:underline" })}
        </div>
      </RunFrame>
    )
  }
  const ms = jobMs(job, now)
  return (
    <RunFrame run={run} owner={owner} now={now} current={job.id} job={job} jobLink={jobLink} summaryLink={summaryLink} onAttempt={onAttempt}>
      <section aria-labelledby={headingId} className="min-w-0 rounded-[10px] border border-border bg-card p-4">
        <div className="flex flex-wrap items-center gap-2">
          <RunStatusIcon status={job.status} conclusion={job.conclusion} />
          <h2 id={headingId} className="text-base font-semibold">
            {job.name}
          </h2>
          {ms !== undefined && <span className="font-mono text-sm text-muted-foreground">{dur(ms)}</span>}
        </div>
        <JobMeta job={job} />
        <div className="mt-4">
          {job.status === "completed" ? (
            <JobLogs repo={run.repo} job={job} search={search} />
          ) : (
            <div className="flex flex-col gap-2">
              <p className="text-sm text-muted-foreground">Logs appear when the job finishes.</p>
              <StepList steps={job.steps} now={now} live />
            </div>
          )}
        </div>
      </section>
    </RunFrame>
  )
}

export function JobPage() {
  const params = useParams({ from: "/app/actions/$repo/runs/$id/jobs/$jobId" })
  const search = useSearch({ from: "/app/actions/$repo/runs/$id/jobs/$jobId" })
  const jobId = positiveInt(params.jobId)
  return (
    <RunGate repo={params.repo} idParam={params.id} attempt={search.attempt} invalid={jobId === undefined}>
      {(run, owner) => <JobView run={run} owner={owner} jobId={jobId ?? 0} search={search} />}
    </RunGate>
  )
}
