import { Input } from "darkraise-ui/components/input"
import { ChevronRight } from "lucide-react"
import { useEffect, useMemo, useRef, useState, type KeyboardEvent, type ReactNode } from "react"
import type { JobLog, LogLine, LogStep, Step } from "@/api/types"
import { RunStatusIcon } from "@/components/run-status-icon"
import { renderAnsi } from "@/lib/ansi"
import { dur } from "@/lib/format"
import { containingGroup, findMatches, groupRows, windowStart, type LogMatch, type LogRow } from "@/lib/log-view"
import { firstFailedStep } from "@/lib/run-details"

const PREFIX: Record<string, string | undefined> = { error: "Error: ", warning: "Warning: ", notice: "Notice: " }
const ROW_CLASS: Record<string, string | undefined> = { error: "ghr-log-error", warning: "ghr-log-warning" }
const TEXT_CLASS: Record<string, string | undefined> = { command: "ghr-log-command", debug: "ghr-log-muted" }

const groupKey = (step: number, n: number) => `${step}:${n}`

function stepMs(step: Step | undefined): number | undefined {
  if (!step?.started_at || !step.completed_at) return undefined
  return Date.parse(step.completed_at) - Date.parse(step.started_at)
}

// The rows from line index start on. A group that begins above the cut keeps
// its header and the members below it, so they stay folded under it.
function rowsFrom(lines: LogLine[], start: number): LogRow[] {
  const from = lines[start]?.n ?? 0
  return groupRows(lines).flatMap((row): LogRow[] => {
    if (row.line.n >= from) return [row]
    if (row.type === "line") return []
    const kept = row.lines.filter((l) => l.n >= from)
    return kept.length > 0 ? [{ ...row, lines: kept }] : []
  })
}

function Line({ line, query, current, target }: { line: LogLine; query: string; current?: number; target: boolean }) {
  return (
    <div
      data-line={line.n}
      data-target={target ? "true" : undefined}
      className={`flex gap-3 px-2 ${ROW_CLASS[line.kind] ?? ""} ${target ? "ghr-log-target" : ""}`}
    >
      <span className="ghr-log-muted w-12 shrink-0 text-right select-none">{line.n}</span>
      <span className={`min-w-0 break-all whitespace-pre-wrap ${TEXT_CLASS[line.kind] ?? ""}`}>
        {PREFIX[line.kind]}
        {renderAnsi(line.text, query, current)}
      </span>
    </div>
  )
}

export function LogViewer({
  log,
  steps,
  step,
  line,
  toolbar,
  heading,
}: {
  log: JobLog
  steps: Step[]
  step?: number
  line?: number
  toolbar?: ReactNode
  heading?: ReactNode
}) {
  const target = step !== undefined && line !== undefined ? { step, line } : undefined
  const linesOf = (n: number): LogLine[] => log.steps.find((s) => s.number === n)?.lines ?? []

  const [openSteps, setOpenSteps] = useState<Set<number>>(() => {
    const first = step ?? firstFailedStep(steps)
    return new Set(first === undefined ? [] : [first])
  })
  const [openGroups, setOpenGroups] = useState<Set<string>>(() => {
    if (!target) return new Set()
    const g = containingGroup(linesOf(target.step), target.line)
    return new Set(g === undefined ? [] : [groupKey(target.step, g)])
  })
  const [showAll, setShowAll] = useState<Set<number>>(() => new Set())
  const [query, setQuery] = useState("")
  const [cur, setCur] = useState(0)
  const ref = useRef<HTMLDivElement>(null)

  const matches = useMemo(() => findMatches(log, query), [log, query])
  const current: LogMatch | undefined = matches[cur]

  // The current match's step and group show however the owner left them,
  // so a search opens nothing else.
  const currentGroup = current ? containingGroup(linesOf(current.step), current.line) : undefined
  const stepOpen = (n: number) => openSteps.has(n) || current?.step === n
  const groupOpen = (stepN: number, n: number) =>
    openGroups.has(groupKey(stepN, n)) || (current?.step === stepN && currentGroup === n)

  function onQuery(next: string) {
    setQuery(next)
    setCur(0)
  }

  function onKey(e: KeyboardEvent<HTMLInputElement>) {
    if (e.key !== "Enter" || matches.length === 0) return
    e.preventDefault()
    const next = (cur + (e.shiftKey ? matches.length - 1 : 1)) % matches.length
    setCur(next)
  }

  function toggle<T>(set: (f: (s: Set<T>) => Set<T>) => void, key: T) {
    set((s) => {
      const out = new Set(s)
      if (!out.delete(key)) out.add(key)
      return out
    })
  }

  useEffect(() => {
    ref.current?.querySelector('[data-target="true"]')?.scrollIntoView({ block: "center" })
  }, [])

  useEffect(() => {
    if (query) ref.current?.querySelector('mark[data-current="true"]')?.scrollIntoView({ block: "center" })
  }, [query, cur])

  function renderLines(s: LogStep) {
    const keep = current?.step === s.number ? current.line : target?.step === s.number ? target.line : undefined
    const start = showAll.has(s.number) ? 0 : windowStart(s.lines, keep)
    const lineProps = (l: LogLine) => ({
      line: l,
      query,
      current: current && current.step === s.number && current.line === l.n ? current.index : undefined,
      target: target?.step === s.number && target.line === l.n,
    })
    return (
      <div className="ghr-log overflow-x-auto py-2 font-mono text-xs leading-5">
        {start > 0 && (
          <button
            type="button"
            className="ghr-log-command focus-ring-tight px-2 pb-1 hover:underline"
            onClick={() => setShowAll((all) => new Set(all).add(s.number))}
          >
            {`Show ${start} earlier lines`}
          </button>
        )}
        {s.lines.length === 0 && <p className="ghr-log-muted px-2">This step wrote no log lines.</p>}
        {rowsFrom(s.lines, start).map((row) => {
          if (row.type === "line") return <Line key={row.line.n} {...lineProps(row.line)} />
          const key = groupKey(s.number, row.line.n)
          const open = groupOpen(s.number, row.line.n)
          return (
            <div key={row.line.n}>
              <button
                type="button"
                aria-expanded={open}
                onClick={() => toggle(setOpenGroups, key)}
                className="focus-ring-tight flex w-full items-start gap-1 text-left hover:underline"
              >
                <ChevronRight aria-hidden="true" size={14} className={`mt-0.5 ml-12 shrink-0 ${open ? "rotate-90" : ""}`} />
                <span className="min-w-0 break-all whitespace-pre-wrap">
                  {renderAnsi(
                    row.line.text,
                    query,
                    current && current.step === s.number && current.line === row.line.n ? current.index : undefined,
                  )}
                </span>
              </button>
              {open && row.lines.map((l) => <Line key={l.n} {...lineProps(l)} />)}
            </div>
          )
        })}
      </div>
    )
  }

  return (
    <div ref={ref} className="ghr-log-panel overflow-hidden rounded-md border border-border">
      <div className="flex flex-wrap items-center gap-3 border-b border-border px-3 py-2">
        {heading && <div className="mr-auto flex min-w-0 items-center gap-2 text-sm font-semibold">{heading}</div>}
        <Input
          type="search"
          aria-label="Search logs"
          placeholder="Search logs"
          value={query}
          onChange={(e) => onQuery(e.target.value)}
          onKeyDown={onKey}
          className="w-64"
        />
        {query && (
          <span aria-live="polite" className="font-mono text-sm text-muted-foreground">
            {matches.length === 0 ? "0 of 0" : `${cur + 1} of ${matches.length}`}
          </span>
        )}
        {toolbar}
      </div>
      <ol aria-label="Steps" className="flex flex-col">
        {log.steps.map((s) => {
          const info = steps.find((x) => x.number === s.number)
          const open = stepOpen(s.number)
          const ms = stepMs(info)
          return (
            <li key={s.number} className="border-b border-border last:border-b-0">
              <button
                type="button"
                aria-expanded={open}
                onClick={() => toggle(setOpenSteps, s.number)}
                className="focus-ring-tight flex w-full items-center gap-2 px-3 py-1.5 text-left text-sm hover:bg-muted"
              >
                <ChevronRight aria-hidden="true" size={15} className={`shrink-0 text-muted-foreground ${open ? "rotate-90" : ""}`} />
                {info && <RunStatusIcon status={info.status} conclusion={info.conclusion} />}
                <span className="min-w-0 flex-1 break-words">{s.name}</span>
                {ms !== undefined && <span className="font-mono text-xs text-muted-foreground">{dur(ms)}</span>}
              </button>
              {open && <div>{renderLines(s)}</div>}
            </li>
          )
        })}
      </ol>
    </div>
  )
}
