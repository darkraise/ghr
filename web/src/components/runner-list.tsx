import { Link, useNavigate } from "@tanstack/react-router"
import { Spinner } from "darkraise-ui/components/spinner"
import { useRef, type KeyboardEvent } from "react"
import type { Status } from "@/api/types"
import type { DetailTab } from "@/components/runner-panel"
import { elapsed } from "@/lib/format"
import { waitingRepos } from "@/lib/status"

// The light shows capacity like the shell's bar: busy pulses, warm is an
// outline, starting is faint.
const LIGHT: Record<string, string> = {
  busy: "ghr-pulse bg-primary",
  starting: "bg-primary/45",
  idle: "border border-primary",
}

const sentence = (s: string) => s.charAt(0).toUpperCase() + s.slice(1)

export function RunnerList({
  status,
  selected,
  tab,
  now,
}: {
  status: Status | undefined
  selected: string | undefined
  tab: DetailTab
  now: number
}) {
  const navigate = useNavigate()
  const links = useRef(new Map<string, HTMLAnchorElement>())
  if (!status) return <Spinner label="Waiting for the daemon" />

  const ids = status.instances.map((i) => i.id)
  const waiting = waitingRepos(status)
  if (ids.length === 0 && waiting.length === 0) {
    return (
      <div className="flex flex-col items-start gap-2 py-2">
        <p className="text-sm text-muted-foreground">No runners. They start when a job is queued.</p>
        <Link to="/repositories" className="text-sm text-primary hover:underline">
          Repositories
        </Link>
      </div>
    )
  }
  const tabbable = selected !== undefined && ids.includes(selected) ? selected : ids[0]

  // Arrowing replaces the URL, so Back leaves the page instead of walking
  // through every runner passed on the way.
  function onKeyDown(e: KeyboardEvent<HTMLUListElement>) {
    const at = ids.indexOf((document.activeElement as HTMLElement | null)?.dataset.runner ?? "")
    let next: number
    if (e.key === "ArrowDown") next = Math.min(ids.length - 1, at + 1)
    else if (e.key === "ArrowUp") next = Math.max(0, at - 1)
    else if (e.key === "Home") next = 0
    else if (e.key === "End") next = ids.length - 1
    else return
    e.preventDefault()
    const id = ids[next]
    if (id === undefined) return
    links.current.get(id)?.focus()
    void navigate({ to: "/runners/$id", params: { id }, search: { tab }, replace: true })
  }

  return (
    <div className="flex flex-col gap-4">
      {ids.length > 0 && (
        <ul aria-label="Runners" className="flex flex-col rounded-[10px] border border-border bg-card" onKeyDown={onKeyDown}>
          {status.instances.map((i) => (
            <li key={i.id} className="border-b border-border last:border-b-0">
              <Link
                to="/runners/$id"
                params={{ id: i.id }}
                search={{ tab }}
                activeOptions={{ includeSearch: false }}
                data-runner={i.id}
                tabIndex={i.id === tabbable ? 0 : -1}
                ref={(el: HTMLAnchorElement | null) => {
                  if (el) links.current.set(i.id, el)
                  else links.current.delete(i.id)
                }}
                className={`grid grid-cols-[0.625rem_minmax(0,1fr)_auto] items-center gap-x-3 gap-y-0.5 px-3 py-2 text-sm hover:bg-muted/50 ${
                  i.id === selected ? "bg-muted" : ""
                }`}
              >
                <span aria-hidden="true" className={`size-2.5 rounded-[2px] ${LIGHT[i.state] ?? "bg-muted-foreground"}`} />
                <span className="flex min-w-0 flex-wrap items-baseline gap-x-2">
                  <span className="font-mono font-medium">{i.id}</span>
                  <span className="sr-only">{sentence(i.state)}</span>
                  <span className="text-muted-foreground">{i.repo}</span>
                </span>
                <span className="font-mono text-xs text-muted-foreground">{elapsed(i, now)}</span>
                <span />
                <span className="col-span-2 text-muted-foreground">
                  {i.job ? (
                    <>
                      {i.job.name} <span className="font-mono">#{i.job.run_number}</span>
                    </>
                  ) : (
                    <span aria-hidden="true">{sentence(i.state)}</span>
                  )}
                </span>
              </Link>
            </li>
          ))}
        </ul>
      )}
      {waiting.length > 0 && (
        <section aria-labelledby="runners-waiting" className="flex flex-col gap-1">
          <h2 id="runners-waiting" className="text-sm font-medium text-warning">
            Waiting
          </h2>
          <ul className="flex flex-col text-sm">
            {waiting.map((r) => (
              <li key={r.name} className="px-3 py-1">{`${r.name}, ${r.queued} queued, cap ${r.max}`}</li>
            ))}
          </ul>
        </section>
      )}
    </div>
  )
}
