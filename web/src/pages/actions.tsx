import { Link, useNavigate, useSearch } from "@tanstack/react-router"
import { Alert, AlertDescription, AlertTitle } from "darkraise-ui/components/alert"
import { Button } from "darkraise-ui/components/button"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "darkraise-ui/components/select"
import { Spinner } from "darkraise-ui/components/spinner"
import { ToggleGroup, ToggleGroupItem } from "darkraise-ui/components/toggle-group"
import { PageHeader } from "darkraise-ui/layout"
import { useEffect } from "react"
import { useActions, useConfig } from "@/api/hooks"
import type { ActionsRun } from "@/api/types"
import { ErrorLine } from "@/components/page/error-line"
import { RunGroup, RunRow, RunValue } from "@/components/page/run-row"
import { Section } from "@/components/page/section"
import { RunStatusIcon } from "@/components/run-status-icon"
import {
  actionsSummary,
  repoErrorText,
  runMs,
  splitRuns,
  startText,
  STATUS_FILTERS,
  type ActionsSearch,
  type ActionsStatus,
} from "@/lib/actions"
import { dur } from "@/lib/format"
import { groupByDayOf } from "@/lib/history"
import { runCols } from "@/lib/run-cols"
import { useMediaQuery } from "@/lib/use-media-query"
import { useNow } from "@/lib/use-now"
import { errorText } from "@/query"

const ALL = "__all__"

const BRANCH = "rounded-full bg-primary/10 px-2 py-0.5 font-mono text-xs text-primary"
const GHR = "rounded-full bg-muted px-2 py-0.5 font-mono text-xs text-foreground"

function ActionRow({ run, now, wide }: { run: ActionsRun; now: number; wide: boolean }) {
  const by = [run.event, run.actor && `by ${run.actor}`].filter(Boolean).join(" ")
  return (
    <RunRow
      wide={wide}
      icon={<RunStatusIcon status={run.status} conclusion={run.conclusion} />}
      title={
        <Link to="/actions/$repo/runs/$id" params={{ repo: run.repo, id: String(run.id) }} className="hover:text-primary hover:underline">
          {run.title}
          <span className="sr-only">{`, ${run.repo} #${run.run_number}`}</span>
        </Link>
      }
      facts={
        <>
          <span>{`${run.workflow} #${run.run_number}`}</span>
          <span>{run.repo}</span>
          {by && <span>{by}</span>}
          {run.branch && <span className={BRANCH}>{run.branch}</span>}
          {run.ghr && <span className={GHR}>ghr</span>}
        </>
      }
      values={
        <>
          <RunValue kind="time">{startText(run.started_at, now)}</RunValue>
          <RunValue kind="duration">{dur(runMs(run, now))}</RunValue>
        </>
      }
    />
  )
}

export function ActionsPage() {
  const search = useSearch({ from: "/app/actions" })
  const navigate = useNavigate()
  const config = useConfig()
  const actions = useActions()
  const now = useNow()
  const wide = useMediaQuery("(min-width: 768px)")

  const configured = (config.data?.repos ?? []).map((r) => r.name)
  const watched = config.data?.watch_repos ?? []
  const names = [...configured, ...watched]
  const repo = names.find((n) => n.toLowerCase() === (search.repo ?? "").toLowerCase())
  const status: ActionsStatus | undefined = search.status

  // A repository that is neither configured nor watched leaves the URL once
  // the config has loaded, so the address bar matches the page.
  useEffect(() => {
    if (config.data && search.repo && !repo)
      void navigate({
        to: "/actions",
        search: status ? { status } : {},
        replace: true,
      })
  }, [config.data, search.repo, repo, status, navigate])

  function update(next: { repo?: string; status?: ActionsStatus | "" }) {
    const merged: { repo: string; status: ActionsStatus | "" } = {
      repo: repo ?? "",
      status: status ?? "",
      ...next,
    }
    const out: ActionsSearch = {}
    if (merged.repo) out.repo = merged.repo
    if (merged.status) out.status = merged.status
    void navigate({ to: "/actions", search: out })
  }

  const data = actions.data
  const { active, recent } = data ? splitRuns(data.runs, repo, status) : { active: [], recent: [] }
  const failed = data?.repos.filter((r) => r.error) ?? []

  const filters = (
    <>
      <Select value={repo ?? ALL} onValueChange={(v) => update({ repo: v === ALL ? "" : v })}>
        <SelectTrigger aria-label="Repository" className="w-56">
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value={ALL}>All repositories</SelectItem>
          {configured.map((name) => (
            <SelectItem key={name} value={name}>
              {name}
            </SelectItem>
          ))}
          {watched.map((name) => (
            <SelectItem key={name} value={name}>
              {`${name} (watched)`}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
      <ToggleGroup
        type="single"
        size="sm"
        variant="outline"
        value={status ?? ALL}
        aria-label="Status"
        onValueChange={(v) =>
          update({
            status: STATUS_FILTERS.find((s) => s.value === v)?.value ?? "",
          })
        }
      >
        <ToggleGroupItem value={ALL}>All</ToggleGroupItem>
        {STATUS_FILTERS.map((s) => (
          <ToggleGroupItem key={s.value} value={s.value}>
            {s.label}
          </ToggleGroupItem>
        ))}
      </ToggleGroup>
    </>
  )
  let listed = false
  let body
  if (actions.isError && !data) body = <ErrorLine onRetry={() => void actions.refetch()}>{errorText(actions.error)}</ErrorLine>
  else if (!data) body = <Spinner label="Loading" />
  else if (data.repos.length === 0) {
    body = (
      <div className="flex flex-col items-start gap-2">
        <p className="text-sm text-muted-foreground">No repositories are covered yet.</p>
        <div className="flex flex-wrap gap-3 text-sm">
          <Link to="/repositories" className="text-primary hover:underline">
            Add a repository
          </Link>
          <Link to="/repositories" hash="watched" className="text-primary hover:underline">
            Watch a repository
          </Link>
        </div>
      </div>
    )
  } else if (status !== "active" && data.runs.length === 0) {
    body = <p className="text-sm text-muted-foreground">No workflow runs in the covered repositories yet.</p>
  } else if (status !== "active" && active.length === 0 && recent.length === 0) {
    body = (
      <div className="flex flex-col items-start gap-2">
        <p className="text-sm text-muted-foreground">No runs match these filters.</p>
        <Button size="sm" variant="outline" onClick={() => update({ repo: "", status: "" })}>
          Clear filters
        </Button>
      </div>
    )
  } else {
    listed = true
    const shown = active.length + recent.length
    const cols = runCols(wide, false)
    body = (
      <Section
        title={`${shown} workflow ${shown === 1 ? "run" : "runs"}`}
        aside={<div className="ml-auto flex flex-wrap items-center gap-3">{filters}</div>}
        flush
      >
        <div className="overflow-x-auto">
          <table className="w-full border-collapse">
            {(active.length > 0 || status === "active") && (
              <tbody>
                <RunGroup label="In progress" cols={cols} />
                {active.length === 0 ? (
                  <tr>
                    <td colSpan={cols} className="px-4 py-3 text-sm text-muted-foreground">
                      Nothing is running or queued.
                    </td>
                  </tr>
                ) : (
                  active.map((r) => <ActionRow key={`${r.repo}#${r.id}`} run={r} now={now} wide={wide} />)
                )}
              </tbody>
            )}
            {groupByDayOf(recent, (r) => r.started_at, now).map((g) => (
              <tbody key={g.key}>
                <RunGroup label={g.label} cols={cols} />
                {g.rows.map((r) => (
                  <ActionRow key={`${r.repo}#${r.id}`} run={r} now={now} wide={wide} />
                ))}
              </tbody>
            ))}
          </table>
        </div>
      </Section>
    )
  }

  const watchLink = (
    <Link to="/repositories" hash="watched" className="text-sm text-primary hover:underline">
      Watch more repositories
    </Link>
  )
  return (
    <div className="flex flex-col gap-4">
      <PageHeader
        title="Actions"
        description={data ? actionsSummary(data.runs, data.repos.length, repo, new Date(data.fetched_at)) : undefined}
        actions={watchLink}
      />
      {!listed && <div className="flex flex-wrap items-center gap-3">{filters}</div>}
      {failed.length > 0 && (
        <Alert variant="warning">
          <AlertTitle as="h2">Some repositories could not be read</AlertTitle>
          <AlertDescription>
            <ul className="flex flex-col gap-0.5">
              {failed.map((r) => (
                <li key={r.repo}>{repoErrorText(r)}</li>
              ))}
            </ul>
          </AlertDescription>
        </Alert>
      )}
      {actions.isError && data && <ErrorLine onRetry={() => void actions.refetch()}>{errorText(actions.error)}</ErrorLine>}
      {body}
    </div>
  )
}
