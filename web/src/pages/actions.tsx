import { Link, useNavigate, useSearch } from "@tanstack/react-router"
import { Alert, AlertDescription, AlertTitle } from "darkraise-ui/components/alert"
import { Button } from "darkraise-ui/components/button"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "darkraise-ui/components/select"
import { Spinner } from "darkraise-ui/components/spinner"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "darkraise-ui/components/table"
import { ToggleGroup, ToggleGroupItem } from "darkraise-ui/components/toggle-group"
import { PageHeader } from "darkraise-ui/layout"
import { Clock } from "lucide-react"
import { useEffect } from "react"
import { useActions, useConfig } from "@/api/hooks"
import type { ActionsRun } from "@/api/types"
import { ErrorLine } from "@/components/page/error-line"
import { Section } from "@/components/page/section"
import { ResultIcon } from "@/components/result-icon"
import {
  actionsSummary,
  isActive,
  isQueued,
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
import { useMediaQuery } from "@/lib/use-media-query"
import { useNow } from "@/lib/use-now"
import { errorText } from "@/query"

const ALL = "__all__"

function StatusIcon({ run }: { run: ActionsRun }) {
  if (!isActive(run)) return <ResultIcon conclusion={run.conclusion} />
  if (isQueued(run)) return <Clock role="img" aria-label="Queued" size={15} className="shrink-0 text-muted-foreground" />
  return <Spinner size="sm" label={<span className="sr-only">Running</span>} />
}

function RunRow({ run, now, wide }: { run: ActionsRun; now: number; wide: boolean }) {
  const took = dur(runMs(run, now))
  const start = startText(run.started_at, now)
  return (
    <TableRow>
      <TableCell className="w-8 align-top">
        <StatusIcon run={run} />
      </TableCell>
      <TableCell className="align-top">
        <a href={run.html_url} target="_blank" rel="noreferrer" className="font-medium hover:underline">
          {run.title}
          <span className="sr-only">{`, ${run.repo} #${run.run_number}, opens in a new tab`}</span>
        </a>
        <span className="mt-1 flex flex-wrap gap-x-3 gap-y-0.5 text-xs text-muted-foreground">
          <span>{run.workflow}</span>
          <span>{run.repo}</span>
          <span className="font-mono">#{run.run_number}</span>
          {run.branch && <span className="font-mono">{run.branch}</span>}
          {run.event && <span>{run.event}</span>}
          {run.actor && <span>{run.actor}</span>}
        </span>
        {!wide && (
          <span className="mt-1 flex flex-wrap gap-x-3 text-xs text-muted-foreground">
            <span className="font-mono">{took}</span>
            <span className="font-mono">{start}</span>
            {run.ghr && <span className="font-mono">ghr</span>}
          </span>
        )}
      </TableCell>
      {wide && (
        <>
          <TableCell className="text-right align-top font-mono">{took}</TableCell>
          <TableCell className="text-right align-top font-mono">{start}</TableCell>
          <TableCell className="text-right align-top font-mono text-muted-foreground">{run.ghr ? "ghr" : ""}</TableCell>
        </>
      )}
    </TableRow>
  )
}

function RunHead({ wide }: { wide: boolean }) {
  return (
    <TableHeader>
      <TableRow>
        <TableHead>
          <span className="sr-only">Status</span>
        </TableHead>
        <TableHead>Run</TableHead>
        {wide && (
          <>
            <TableHead className="text-right">Duration</TableHead>
            <TableHead className="text-right">Started</TableHead>
            <TableHead>
              <span className="sr-only">Runner</span>
            </TableHead>
          </>
        )}
      </TableRow>
    </TableHeader>
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
    if (config.data && search.repo && !repo) void navigate({ to: "/actions", search: status ? { status } : {}, replace: true })
  }, [config.data, search.repo, repo, status, navigate])

  function update(next: { repo?: string; status?: ActionsStatus | "" }) {
    const merged: { repo: string; status: ActionsStatus | "" } = { repo: repo ?? "", status: status ?? "", ...next }
    const out: ActionsSearch = {}
    if (merged.repo) out.repo = merged.repo
    if (merged.status) out.status = merged.status
    void navigate({ to: "/actions", search: out })
  }

  const data = actions.data
  const { active, recent } = data ? splitRuns(data.runs, repo, status) : { active: [], recent: [] }
  const failed = data?.repos.filter((r) => r.error) ?? []
  const cols = wide ? 5 : 2

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
    body = (
      <>
        {(active.length > 0 || status === "active") && (
          <Section title="In progress">
            {active.length === 0 ? (
              <p className="text-sm text-muted-foreground">Nothing is running or queued.</p>
            ) : (
              <div className="overflow-x-auto">
                <Table>
                  <RunHead wide={wide} />
                  <TableBody>
                    {active.map((r) => (
                      <RunRow key={`${r.repo}#${r.id}`} run={r} now={now} wide={wide} />
                    ))}
                  </TableBody>
                </Table>
              </div>
            )}
          </Section>
        )}
        {recent.length > 0 && (
          <Section title="Recent">
            <div className="overflow-x-auto">
              <Table>
                <RunHead wide={wide} />
                {groupByDayOf(recent, (r) => r.started_at, now).map((g) => (
                  <TableBody key={g.key}>
                    <TableRow>
                      <th scope="rowgroup" colSpan={cols} className="pt-4 pb-1 text-left text-sm font-medium text-muted-foreground">
                        {g.label}
                      </th>
                    </TableRow>
                    {g.rows.map((r) => (
                      <RunRow key={`${r.repo}#${r.id}`} run={r} now={now} wide={wide} />
                    ))}
                  </TableBody>
                ))}
              </Table>
            </div>
          </Section>
        )}
      </>
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
      <div className="flex flex-wrap items-center gap-3">
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
          onValueChange={(v) => update({ status: STATUS_FILTERS.find((s) => s.value === v)?.value ?? "" })}
        >
          <ToggleGroupItem value={ALL}>All</ToggleGroupItem>
          {STATUS_FILTERS.map((s) => (
            <ToggleGroupItem key={s.value} value={s.value}>
              {s.label}
            </ToggleGroupItem>
          ))}
        </ToggleGroup>
      </div>
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
      {body}
    </div>
  )
}
