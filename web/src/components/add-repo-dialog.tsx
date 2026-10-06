import { useQueryClient } from "@tanstack/react-query"
import { Badge } from "darkraise-ui/components/badge"
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
import { Switch } from "darkraise-ui/components/switch"
import { useState } from "react"
import { api } from "@/api/client"
import { keys, useAvailableRepos, useStatus } from "@/api/hooks"
import type { AddRepoRequest } from "@/api/types"
import { TagField } from "@/components/tag-field"
import { errorText } from "@/query"

function clampMax(text: string): number {
  return Math.min(99, Math.max(0, Math.trunc(Number(text))))
}

export function AddRepoDialog({ open, onClose }: { open: boolean; onClose: () => void }) {
  const repos = useAvailableRepos(open)
  const status = useStatus()
  const queryClient = useQueryClient()
  const [filter, setFilter] = useState("")
  const [picked, setPicked] = useState("")
  const [max, setMax] = useState("")
  const [labels, setLabels] = useState<string[]>([])
  const [allowPublic, setAllowPublic] = useState(false)
  const [error, setError] = useState("")
  const [busy, setBusy] = useState(false)

  function close() {
    setFilter("")
    setPicked("")
    setMax("")
    setLabels([])
    setAllowPublic(false)
    setError("")
    onClose()
  }

  async function add() {
    if (status.isError) return setError("the daemon is unreachable")
    if (!picked) return setError("pick a repository first")
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
    toast.success(`added ${picked}`)
    void queryClient.invalidateQueries({ queryKey: keys.status })
    void queryClient.invalidateQueries({ queryKey: keys.config })
    close()
  }

  const needle = filter.trim().toLowerCase()
  const items = (repos.data ?? []).filter((r) => r.name.toLowerCase().includes(needle))
  let picker
  if (repos.isError) {
    picker = (
      <div className="flex items-center gap-2 text-sm">
        <span className="text-destructive">✖ {errorText(repos.error)}</span>
        <Button size="sm" variant="outline" onClick={() => void repos.refetch()}>
          Retry
        </Button>
      </div>
    )
  } else if (!repos.data) {
    picker = <p className="text-sm text-muted-foreground">loading repositories…</p>
  } else {
    picker = (
      <div className="flex flex-col gap-2">
        <Input aria-label="Filter repositories" placeholder="type to filter" value={filter} onChange={(e) => setFilter(e.target.value)} />
        <div role="listbox" aria-label="Repositories" className="max-h-64 overflow-auto rounded-md border">
          {repos.data.length === 0 && <p className="p-2 text-sm text-muted-foreground">nothing to pick</p>}
          {repos.data.length > 0 && items.length === 0 && <p className="p-2 text-sm text-muted-foreground">no match</p>}
          {items.map((r) => (
            <button
              key={r.name}
              type="button"
              role="option"
              aria-selected={picked === r.name}
              disabled={r.configured}
              onClick={() => setPicked(r.name)}
              className="flex w-full items-center gap-2 px-2 py-1 text-left text-sm hover:bg-muted disabled:opacity-50 aria-selected:bg-muted"
            >
              <span>{r.name}</span>
              <Badge variant={r.private ? "secondary" : "amber"} size="sm">
                {r.private ? "private" : "public"}
              </Badge>
              {r.configured && <span className="text-muted-foreground">added</span>}
            </button>
          ))}
        </div>
      </div>
    )
  }

  return (
    <Dialog
      open={open}
      onOpenChange={(o) => {
        if (!o) close()
      }}
    >
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Add repository</DialogTitle>
        </DialogHeader>
        <DialogBody className="flex flex-col gap-4">
          <div className="flex flex-col gap-1">
            <span className="text-sm font-medium">Repository</span>
            {picked ? <strong>{picked}</strong> : <span className="text-sm text-muted-foreground">pick one below</span>}
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
          <p className="text-sm text-amber-600">⚠ self-hosted runners on a public repo can run anyone's code</p>
          {error && (
            <p role="alert" className="text-sm text-destructive">
              ✖ {error}
            </p>
          )}
        </DialogBody>
        <DialogFooter>
          <Button variant="outline" onClick={close}>
            Cancel
          </Button>
          <Button disabled={!picked || busy || status.isError} onClick={() => void add()}>
            {busy ? "Adding…" : "Add"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
