import { useQueryClient } from "@tanstack/react-query"
import { Button } from "darkraise-ui/components/button"
import {
  Dialog,
  DialogBody,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "darkraise-ui/components/dialog"
import { Input } from "darkraise-ui/components/input"
import { Label } from "darkraise-ui/components/label"
import { toast } from "darkraise-ui/components/sonner"
import { Spinner } from "darkraise-ui/components/spinner"
import { Switch } from "darkraise-ui/components/switch"
import { TriangleAlert } from "lucide-react"
import { useState, type KeyboardEvent } from "react"
import { api } from "@/api/client"
import { keys, useAvailableRepos, useStatus } from "@/api/hooks"
import type { AddRepoRequest } from "@/api/types"
import { ErrorLine } from "@/components/page/error-line"
import { TagField } from "@/components/tag-field"
import { errorText } from "@/query"

function clampMax(text: string): number {
  return Math.min(99, Math.max(0, Math.trunc(Number(text))))
}

export function AddRepoDialog({ open, onClose }: { open: boolean; onClose: () => void }) {
  return (
    <Dialog
      open={open}
      onOpenChange={(o) => {
        if (!o) onClose()
      }}
    >
      {/* Mounted only while open: each opening starts clean, and a request
          that ends after closing has nowhere to leave its error. */}
      <DialogContent>{open && <AddRepoForm onClose={onClose} />}</DialogContent>
    </Dialog>
  )
}

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

function AddRepoForm({ onClose }: { onClose: () => void }) {
  const repos = useAvailableRepos(true)
  const status = useStatus()
  const queryClient = useQueryClient()
  const [filter, setFilter] = useState("")
  const [picked, setPicked] = useState("")
  const [max, setMax] = useState("")
  const [labels, setLabels] = useState<string[]>([])
  const [allowPublic, setAllowPublic] = useState(false)
  const [error, setError] = useState("")
  const [busy, setBusy] = useState(false)

  async function add() {
    if (!picked) return setError("Pick a repository first")
    const req: AddRepoRequest = { name: picked, allow_public: allowPublic }
    if (max.trim() !== "" && Number.isFinite(Number(max))) req.max = clampMax(max)
    if (labels.length > 0) req.labels = labels
    setBusy(true)
    setError("")
    try {
      await api.addRepo(req)
    } catch (err) {
      setError(errorText(err))
      return
    } finally {
      setBusy(false)
    }
    toast.success(`Added ${picked}`)
    void queryClient.invalidateQueries({ queryKey: keys.status })
    void queryClient.invalidateQueries({ queryKey: keys.config })
    onClose()
  }

  const needle = filter.trim().toLowerCase()
  const items = (repos.data ?? []).filter((r) => r.name.toLowerCase().includes(needle))
  const tabStop = items.find((r) => r.name === picked && !r.configured)?.name ?? items.find((r) => !r.configured)?.name
  let picker
  if (repos.isError) {
    picker = <ErrorLine onRetry={() => void repos.refetch()}>{errorText(repos.error)}</ErrorLine>
  } else if (!repos.data) {
    picker = <Spinner label="Loading repositories" />
  } else {
    picker = (
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
                disabled={r.configured}
                tabIndex={r.name === tabStop ? 0 : -1}
                onClick={() => setPicked(r.name)}
                className="flex w-full items-center gap-2 px-2 py-1 text-left text-sm hover:bg-muted focus-visible:bg-muted focus-visible:outline-none disabled:opacity-50 aria-selected:bg-muted"
              >
                <span>{r.name}</span>
                {!r.private && <span className="text-muted-foreground">public</span>}
                {r.configured && <span className="text-muted-foreground">added</span>}
              </button>
            ))}
          </div>
        )}
      </div>
    )
  }

  return (
    <>
      <DialogHeader>
        <DialogTitle>Add repository</DialogTitle>
      </DialogHeader>
      <DialogBody className="flex flex-col gap-4">
        <div className="flex flex-col gap-1">
          <span className="text-sm font-medium">Repository</span>
          {picked ? <strong>{picked}</strong> : <span className="text-sm text-muted-foreground">Pick one below</span>}
        </div>
        {picker}
        <div className="flex flex-col gap-1">
          <Label htmlFor="add-max">Max</Label>
          <Input
            id="add-max"
            type="number"
            min={0}
            max={99}
            placeholder={status.data?.mode === "all" ? "∞ (default)" : "1 (default)"}
            value={max}
            onChange={(e) => setMax(e.target.value)}
          />
        </div>
        <div className="flex flex-col gap-1">
          <span className="text-sm font-medium">Labels</span>
          <TagField label="Labels" value={labels} onChange={setLabels} />
        </div>
        <div className="flex items-center gap-2">
          <Switch id="allow-public" checked={allowPublic} onCheckedChange={setAllowPublic} />
          <Label htmlFor="allow-public">Allow public repo</Label>
        </div>
        <p className="flex items-center gap-1.5 text-sm text-muted-foreground">
          <TriangleAlert size={15} aria-hidden="true" className="shrink-0 text-warning" />
          Self-hosted runners on a public repo can run anyone's code
        </p>
        {error && <ErrorLine>{error}</ErrorLine>}
      </DialogBody>
      <DialogFooter>
        <Button variant="outline" onClick={onClose}>
          Cancel
        </Button>
        <Button loading={busy} disabled={!picked || busy} onClick={() => void add()}>
          Add
        </Button>
      </DialogFooter>
    </>
  )
}
