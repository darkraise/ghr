import { Link, useNavigate, useSearch } from "@tanstack/react-router"
import { Button } from "darkraise-ui/components/button"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "darkraise-ui/components/select"
import { Spinner } from "darkraise-ui/components/spinner"
import { ToggleGroup, ToggleGroupItem } from "darkraise-ui/components/toggle-group"
import { Tooltip, TooltipContent, TooltipTrigger } from "darkraise-ui/components/tooltip"
import { PageHeader } from "darkraise-ui/layout"
import { Copy, ExternalLink } from "lucide-react"
import { useState } from "react"
import { HISTORY_LIMIT, useActivity, useConfig, useHistory } from "@/api/hooks"
import type { ActivityBucket, HistoryEntry } from "@/api/types"
import { BucketsChart } from "@/components/buckets-chart"
import { ErrorLine } from "@/components/page/error-line"
import { RunGroup, RunRow, RunValue } from "@/components/page/run-row"
import { Section } from "@/components/page/section"
import { ResultIcon } from "@/components/result-icon"
import { WindowControl } from "@/components/window-control"
import { windowWords } from "@/lib/activity-view"
import { copyWithToast } from "@/lib/clipboard"
import { dur, hhmm } from "@/lib/format"
import {
  groupByDay,
  HISTORY_WINDOWS,
  historySummary,
  inBucket,
  pickText,
  RESULTS,
  took,
  type HistoryResult,
  type HistorySearch,
  type HistoryWindow,
} from "@/lib/history"
import { runCols } from "@/lib/run-cols"
import { useMediaQuery } from "@/lib/use-media-query"
import { useNow } from "@/lib/use-now"
import { useWidth } from "@/lib/use-width"
import { errorText } from "@/query"

const ALL = "__all__"

function Legend() {
  const items: [string, string][] = [
    ["Busy", "bg-primary"],
    ["Succeeded", "bg-success/60"],
    ["Failed", "bg-destructive/70"],
  ]
  return (
    <ul aria-label="Legend" className="ml-auto flex flex-wrap gap-x-4 gap-y-1 text-xs text-muted-foreground">
      <li>All results</li>
      {items.map(([label, swatch]) => (
        <li key={label} className="flex items-center gap-1.5">
          <span aria-hidden="true" className={`size-2.5 rounded-[2px] ${swatch}`} />
          {label}
        </li>
      ))}
    </ul>
  )
}

function HistoryRow({ entry, longest, wide }: { entry: HistoryEntry; longest: number; wide: boolean }) {
  const ms = took(entry)
  const url = entry.html_url
  const name = `${entry.job_name} #${entry.run_number}`
  return (
    <RunRow
      wide={wide}
      icon={<ResultIcon conclusion={entry.conclusion} />}
      title={
        entry.run_id > 0 ? (
          <Link to="/actions/$repo/runs/$id" params={{ repo: entry.repo, id: String(entry.run_id) }} className="hover:text-primary hover:underline">
            {entry.job_name}
          </Link>
        ) : (
          entry.job_name
        )
      }
      facts={
        <>
          <span>{entry.workflow}</span>
          <span>{entry.repo}</span>
          <span className="font-mono">#{entry.run_number}</span>
        </>
      }
      values={
        <>
          <RunValue kind="time">{hhmm(new Date(entry.finished_at))}</RunValue>
          <span className="flex flex-col items-end gap-1">
            <RunValue kind="duration">{dur(ms)}</RunValue>
            <span data-duration-bar aria-hidden="true" className="block h-1.5 w-20 rounded-[2px] bg-muted">
              <span className="block h-1.5 rounded-[2px] bg-foreground/40" style={{ width: `${Math.round((ms / longest) * 100)}%` }} />
            </span>
          </span>
        </>
      }
      actions={
        url ? (
          <span className="flex justify-end gap-1">
            <Tooltip>
              <TooltipTrigger asChild>
                <Button size="icon" variant="ghost" asChild>
                  <a href={url} target="_blank" rel="noreferrer" aria-label={`Open run ${name}`}>
                    <ExternalLink size={15} aria-hidden="true" />
                  </a>
                </Button>
              </TooltipTrigger>
              <TooltipContent>Open run</TooltipContent>
            </Tooltip>
            <Tooltip>
              <TooltipTrigger asChild>
                <Button size="icon" variant="ghost" aria-label={`Copy run URL of ${name}`} onClick={() => void copyWithToast(url, "run URL")}>
                  <Copy size={15} aria-hidden="true" />
                </Button>
              </TooltipTrigger>
              <TooltipContent>Copy run URL</TooltipContent>
            </Tooltip>
          </span>
        ) : (
          <span />
        )
      }
    />
  )
}

export function HistoryPage() {
  const search = useSearch({ from: "/app/history" })
  const navigate = useNavigate()
  const repo = search.repo ?? ""
  const result: HistoryResult | "" = search.result ?? ""
  const span: HistoryWindow = search.window ?? "7d"
  const config = useConfig()
  const activity = useActivity(span, repo)
  // The table covers what the chart covers: it waits for this window's
  // response and starts at its from.
  const since = activity.data && !activity.isPlaceholderData ? activity.data.from : undefined
  const history = useHistory(repo, result, since)
  // Rows kept from the previous filter while the next ones load would sit
  // under the wrong controls.
  const settled = history.isPlaceholderData ? undefined : history.data
  const wide = useMediaQuery("(min-width: 768px)")
  const now = useNow()
  const [ref, width] = useWidth<HTMLDivElement>(960)
  const [pick, setPick] = useState<ActivityBucket | null>(null)

  function update(next: { repo?: string; result?: HistoryResult | ""; window?: HistoryWindow }) {
    setPick(null)
    const merged = { repo, result, window: span, ...next }
    const out: HistorySearch = {}
    if (merged.repo) out.repo = merged.repo
    if (merged.result) out.result = merged.result
    if (merged.window !== "7d") out.window = merged.window
    void navigate({ to: "/history", search: out })
  }

  const names = (config.data?.repos ?? []).map((r) => r.name)
  const repos = repo && !names.includes(repo) ? [...names, repo] : names
  const all = settled ?? []
  // A picked bucket that has rolled out of the window no longer filters.
  const picked = pick && activity.data?.buckets.some((b) => b.start === pick.start) ? pick : null
  const rows = picked ? inBucket(all, picked) : all
  const groups = groupByDay(rows, now)
  const longest = Math.max(1, ...rows.map(took))
  const filtered = repo !== "" || result !== ""

  let body
  if (history.isError) body = <ErrorLine>{errorText(history.error)}</ErrorLine>
  else if (!settled) {
    body = activity.isError ? <p className="text-sm text-muted-foreground">The table loads once the chart does.</p> : <Spinner label="Loading" />
  } else if (rows.length === 0) {
    body = picked ? (
      <p className="text-sm text-muted-foreground">No jobs finished in this span.</p>
    ) : filtered ? (
      <div className="flex flex-col items-start gap-2">
        <p className="text-sm text-muted-foreground">No jobs match these filters</p>
        <Button size="sm" variant="outline" onClick={() => update({ repo: "", result: "" })}>
          Clear filters
        </Button>
      </div>
    ) : (
      <p className="text-sm text-muted-foreground">{`No finished jobs in ${windowWords(span)}`}</p>
    )
  } else {
    const cols = runCols(wide, true)
    body = (
      <div className="overflow-x-auto">
        <table className="w-full border-collapse">
          {groups.map((g) => (
            <tbody key={g.key}>
              <RunGroup label={g.label} cols={cols} />
              {g.rows.map((h) => (
                <HistoryRow key={h.id} entry={h} longest={longest} wide={wide} />
              ))}
            </tbody>
          ))}
        </table>
      </div>
    )
  }

  return (
    <div className="flex flex-col gap-4">
      <PageHeader title="History" />
      <div className="flex flex-wrap items-center gap-3">
        <Select value={repo || ALL} onValueChange={(v) => update({ repo: v === ALL ? "" : v })}>
          <SelectTrigger aria-label="Repository" className="w-52">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value={ALL}>All repositories</SelectItem>
            {repos.map((name) => (
              <SelectItem key={name} value={name}>
                {name}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <ToggleGroup
          type="single"
          size="sm"
          variant="outline"
          value={result || ALL}
          aria-label="Result"
          onValueChange={(v) => update({ result: RESULTS.find((r) => r.value === v)?.value ?? "" })}
        >
          <ToggleGroupItem value={ALL}>All</ToggleGroupItem>
          {RESULTS.map((r) => (
            <ToggleGroupItem key={r.value} value={r.value}>
              {r.label}
            </ToggleGroupItem>
          ))}
        </ToggleGroup>
        <div className="ml-auto">
          <WindowControl value={span} onChange={(w) => update({ window: w })} options={HISTORY_WINDOWS} />
        </div>
      </div>
      <Section title="Jobs" aside={<Legend />}>
        {activity.isError && <ErrorLine onRetry={() => void activity.refetch()}>{errorText(activity.error)}</ErrorLine>}
        <div ref={ref} className="overflow-x-auto">
          {activity.data ? (
            <BucketsChart
              activity={activity.data}
              now={now}
              width={width}
              tracks="jobs"
              picked={picked?.start ?? null}
              onPick={(b) => setPick((p) => (p?.start === b.start ? null : b))}
            />
          ) : (
            !activity.isError && <Spinner label="Loading" />
          )}
        </div>
      </Section>
      <Section title="Finished jobs" aside={settled ? <span className="text-sm text-muted-foreground">{historySummary(rows)}</span> : undefined} flush>
        {picked && (
          <div className="flex flex-wrap items-center gap-2 border-b border-border px-4 py-2.5 text-sm">
            <span>{pickText(picked)}</span>
            <Button size="sm" variant="outline" onClick={() => setPick(null)}>
              Show whole window
            </Button>
          </div>
        )}
        {all.length >= HISTORY_LIMIT && (
          <p className="border-b border-border px-4 py-2.5 text-sm text-muted-foreground">Showing the newest 500 jobs in this window.</p>
        )}
        {!history.isError && settled && rows.length > 0 ? body : <div className="p-4">{body}</div>}
      </Section>
    </div>
  )
}
