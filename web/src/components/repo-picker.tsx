import { Input } from "darkraise-ui/components/input"
import { Spinner } from "darkraise-ui/components/spinner"
import { useState, type KeyboardEvent } from "react"
import { useAvailableRepos } from "@/api/hooks"
import type { AvailableRepo } from "@/api/types"
import { ErrorLine } from "@/components/page/error-line"
import { errorText } from "@/query"

const KEY_STEP: Record<string, (i: number, n: number) => number> = {
  ArrowDown: (i, n) => Math.min(n - 1, i + 1),
  ArrowUp: (i) => Math.max(0, i - 1),
  Home: () => 0,
  End: (_i, n) => n - 1,
}

function moveFocus(e: KeyboardEvent<HTMLDivElement>) {
  const step = KEY_STEP[e.key]
  if (!step) return
  e.preventDefault()
  const options = Array.from(e.currentTarget.querySelectorAll<HTMLButtonElement>('[role="option"]:not(:disabled)'))
  if (options.length === 0) return
  const at = options.findIndex((o) => o === document.activeElement)
  options[step(at, options.length)]?.focus()
}

// The add dialog keeps watched entries pickable, because adding a watched
// repository takes it over; the watch dialog disables them.
export function RepoPicker({
  picked,
  onPick,
  disableWatched = false,
}: {
  picked: string
  onPick: (name: string) => void
  disableWatched?: boolean
}) {
  const repos = useAvailableRepos(true)
  const [filter, setFilter] = useState("")
  const off = (r: AvailableRepo) => r.configured || (disableWatched && r.watched)
  const needle = filter.trim().toLowerCase()
  const items = (repos.data ?? []).filter((r) => r.name.toLowerCase().includes(needle))
  const tabStop = items.find((r) => r.name === picked && !off(r))?.name ?? items.find((r) => !off(r))?.name

  if (repos.isError) return <ErrorLine onRetry={() => void repos.refetch()}>{errorText(repos.error)}</ErrorLine>
  if (!repos.data) return <Spinner label="Loading repositories" />
  return (
    <div className="flex flex-col gap-2">
      <Input aria-label="Filter repositories" placeholder="Type to filter" value={filter} onChange={(e) => setFilter(e.target.value)} />
      {repos.data.length === 0 && <p className="text-sm text-muted-foreground">Nothing to pick</p>}
      {repos.data.length > 0 && items.length === 0 && <p className="text-sm text-muted-foreground">No match</p>}
      {items.length > 0 && (
        <div role="listbox" aria-label="Repositories" className="max-h-64 overflow-auto rounded-md border" onKeyDown={moveFocus}>
          {items.map((r) => (
            <button
              key={r.name}
              type="button"
              role="option"
              aria-selected={picked === r.name}
              disabled={off(r)}
              tabIndex={r.name === tabStop ? 0 : -1}
              onClick={() => onPick(r.name)}
              className="flex w-full items-center gap-2 px-2 py-1 text-left text-sm hover:bg-muted focus-visible:bg-muted focus-visible:outline-none disabled:opacity-50 aria-selected:bg-muted"
            >
              <span>{r.name}</span>
              {!r.private && <span className="text-muted-foreground">public</span>}
              {r.configured && <span className="text-muted-foreground">added</span>}
              {!r.configured && r.watched && <span className="text-muted-foreground">watched</span>}
            </button>
          ))}
        </div>
      )}
    </div>
  )
}
