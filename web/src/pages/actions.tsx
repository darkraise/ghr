import { Link, useNavigate, useSearch } from "@tanstack/react-router"
import { Alert, AlertDescription, AlertTitle } from "darkraise-ui/components/alert"
import { Button } from "darkraise-ui/components/button"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "darkraise-ui/components/select"
import { Spinner } from "darkraise-ui/components/spinner"
import { ToggleGroup, ToggleGroupItem } from "darkraise-ui/components/toggle-group"
import { PageHeader } from "darkraise-ui/layout"
import { useEffect } from "react"
import { useActions, useConfig } from "@/api/hooks"
import { ActionsRunTable } from "@/components/actions-run-table"
import { ErrorLine } from "@/components/page/error-line"
import { Section } from "@/components/page/section"
import {
  actionsSummary,
  repoErrorText,
  splitRuns,
  STATUS_FILTERS,
  type ActionsSearch,
  type ActionsStatus,
} from "@/lib/actions"
import { ALL_REPOS } from "@/lib/repos"
import { useMediaQuery } from "@/lib/use-media-query"
import { useNow } from "@/lib/use-now"
import { errorText } from "@/query"

const ALL = "__all__"

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
  // Until the config answers there is nothing to match against, so the URL's
  // repository stands.
  const repo = config.data ? names.find((n) => n.toLowerCase() === (search.repo ?? "").toLowerCase()) : search.repo
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
      <Select value={repo ?? ALL_REPOS} onValueChange={(v) => update({ repo: v === ALL_REPOS ? "" : v })}>
        <SelectTrigger aria-label="Repository" className="w-56">
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value={ALL_REPOS}>All repositories</SelectItem>
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
          {repo && !names.includes(repo) && <SelectItem value={repo}>{repo}</SelectItem>}
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
  // One box holds the filters in every state, so they keep their place and
  // their focus while the body under them changes. The count is unknown
  // until the first read answers.
  let title = data ? "0 workflow runs" : "Workflow runs"
  let flush = false
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
    const shown = active.length + recent.length
    title = `${shown} workflow ${shown === 1 ? "run" : "runs"}`
    flush = true
    body = <ActionsRunTable active={active} recent={recent} now={now} wide={wide} showActive={status === "active"} />
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
      <Section title={title} aside={<div className="ml-auto flex flex-wrap items-center gap-3">{filters}</div>} flush={flush}>
        {body}
      </Section>
    </div>
  )
}
