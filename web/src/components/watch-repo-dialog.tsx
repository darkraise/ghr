import { useQueryClient } from "@tanstack/react-query"
import { Button } from "darkraise-ui/components/button"
import { Dialog, DialogBody, DialogContent, DialogFooter, DialogHeader, DialogTitle } from "darkraise-ui/components/dialog"
import { toast } from "darkraise-ui/components/sonner"
import { useState } from "react"
import { api } from "@/api/client"
import { keys } from "@/api/hooks"
import { ErrorLine } from "@/components/page/error-line"
import { RepoPicker } from "@/components/repo-picker"
import { errorText } from "@/query"

export function WatchRepoDialog({ open, onClose }: { open: boolean; onClose: () => void }) {
  return (
    <Dialog
      open={open}
      onOpenChange={(o) => {
        if (!o) onClose()
      }}
    >
      <DialogContent>{open && <WatchRepoForm onClose={onClose} />}</DialogContent>
    </Dialog>
  )
}

function WatchRepoForm({ onClose }: { onClose: () => void }) {
  const queryClient = useQueryClient()
  const [picked, setPicked] = useState("")
  const [error, setError] = useState("")
  const [busy, setBusy] = useState(false)

  async function watch() {
    if (!picked) return
    setBusy(true)
    setError("")
    try {
      await api.watchRepo(picked)
    } catch (err) {
      setError(errorText(err))
      return
    } finally {
      setBusy(false)
    }
    toast.success(`Watching ${picked}`)
    void queryClient.invalidateQueries({ queryKey: keys.config })
    void queryClient.invalidateQueries({ queryKey: keys.actions })
    onClose()
  }

  return (
    <>
      <DialogHeader>
        <DialogTitle>Watch a repository</DialogTitle>
      </DialogHeader>
      <DialogBody className="flex flex-col gap-4">
        <p className="text-sm text-muted-foreground">Its workflow runs show on the Actions page. ghr runs no runners for it.</p>
        <div className="flex flex-col gap-1">
          <span className="text-sm font-medium">Repository</span>
          {picked ? <strong>{picked}</strong> : <span className="text-sm text-muted-foreground">Pick one below</span>}
        </div>
        <RepoPicker picked={picked} onPick={setPicked} disableWatched />
        {error && <ErrorLine>{error}</ErrorLine>}
      </DialogBody>
      <DialogFooter>
        <Button variant="outline" onClick={onClose}>
          Cancel
        </Button>
        <Button loading={busy} disabled={!picked || busy} onClick={() => void watch()}>
          Watch
        </Button>
      </DialogFooter>
    </>
  )
}
