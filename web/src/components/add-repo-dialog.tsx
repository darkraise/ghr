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
import { Switch } from "darkraise-ui/components/switch"
import { useState } from "react"
import { api } from "@/api/client"
import { keys, useAvailableRepos, useStatus } from "@/api/hooks"
import type { AddRepoRequest } from "@/api/types"
import { ConfirmDialog, type Confirm } from "@/components/confirm-dialog"
import { ErrorLine } from "@/components/page/error-line"
import { RepoPicker } from "@/components/repo-picker"
import { TagField } from "@/components/tag-field"
import { PUBLIC_HELP, servePublicConfirm } from "@/lib/repos"
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

function AddRepoForm({ onClose }: { onClose: () => void }) {
  const status = useStatus()
  const queryClient = useQueryClient()
  const [picked, setPicked] = useState("")
  const [max, setMax] = useState("")
  const [labels, setLabels] = useState<string[]>([])
  const [allowPublic, setAllowPublic] = useState(false)
  const [error, setError] = useState("")
  const [busy, setBusy] = useState(false)
  const [question, setQuestion] = useState<Confirm | null>(null)
  const available = useAvailableRepos(true)
  const isPublic = available.data?.find((r) => r.name === picked)?.private === false

  function pick(name: string) {
    setPicked(name)
    setAllowPublic(false)
  }

  function add() {
    if (!picked) return setError("Pick a repository first")
    if (isPublic) return setQuestion(servePublicConfirm(picked, () => void send()))
    void send()
  }

  async function send() {
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

  return (
    <>
      <DialogHeader>
        <DialogTitle>Add repository</DialogTitle>
      </DialogHeader>
      <DialogBody className="flex flex-col gap-4">
        <div className="flex flex-col gap-1">
          <span className="text-sm font-semibold">Repository</span>
          {picked ? <strong>{picked}</strong> : <span className="text-sm text-muted-foreground">Pick one below</span>}
        </div>
        <RepoPicker picked={picked} onPick={pick} />
        <div className="flex flex-col gap-1">
          <Label htmlFor="add-max">Max</Label>
          <Input
            id="add-max"
            type="number"
            min={0}
            max={99}
            placeholder={status.data?.mode === "all" ? "No limit (default)" : "1 (default)"}
            value={max}
            onChange={(e) => setMax(e.target.value)}
          />
        </div>
        <div className="flex flex-col gap-1">
          <span className="text-sm font-semibold">Labels</span>
          <TagField label="Labels" value={labels} onChange={setLabels} />
        </div>
        {isPublic && (
          <div className="flex flex-col gap-1">
            <div className="flex items-center gap-2">
              <Switch id="allow-public" checked={allowPublic} onCheckedChange={setAllowPublic} aria-describedby="allow-public-help" />
              <Label htmlFor="allow-public">Serve when public</Label>
            </div>
            <p id="allow-public-help" className="text-sm text-muted-foreground">
              {PUBLIC_HELP}
            </p>
            {!allowPublic && <p className="text-sm text-warning">Turn on Serve when public to add a public repository.</p>}
          </div>
        )}
        {error && <ErrorLine>{error}</ErrorLine>}
      </DialogBody>
      <DialogFooter>
        <Button variant="outline" onClick={onClose}>
          Cancel
        </Button>
        <Button loading={busy} disabled={!picked || busy || (isPublic && !allowPublic)} onClick={add}>
          Add
        </Button>
      </DialogFooter>
      <ConfirmDialog confirm={question} onClose={() => setQuestion(null)} />
    </>
  )
}
