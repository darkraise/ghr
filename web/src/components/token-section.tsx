import { useQueryClient } from "@tanstack/react-query"
import { Button } from "darkraise-ui/components/button"
import {
  Dialog,
  DialogBody,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "darkraise-ui/components/dialog"
import { Input } from "darkraise-ui/components/input"
import { Label } from "darkraise-ui/components/label"
import { toast } from "darkraise-ui/components/sonner"
import { useState } from "react"
import { api } from "@/api/client"
import { keys, useStatus, useToken } from "@/api/hooks"
import { ConfirmDialog, type Confirm } from "@/components/confirm-dialog"
import { ErrorLine } from "@/components/page/error-line"
import { Field, FieldGroup } from "@/components/page/field"
import { StateText } from "@/components/page/state-text"
import { ago } from "@/lib/format"
import { expiryText, rateText, tokenWord } from "@/lib/token"
import { useNow } from "@/lib/use-now"
import { errorText } from "@/query"

const NOTE =
  "Replacing the token takes effect at once, outside the save bar. It checks read access to the first repository only; registration permissions are checked when a runner next starts."

export function TokenSection() {
  const token = useToken()
  const status = useStatus()
  const now = useNow()
  const [open, setOpen] = useState(false)
  const t = token.data

  return (
    <FieldGroup id="token" title="GitHub token" note={NOTE}>
      {token.isError && (
        <div className="py-3">
          <ErrorLine onRetry={() => void token.refetch()}>{errorText(token.error)}</ErrorLine>
        </div>
      )}
      <Field label="Status">
        {t ? (
          <>
            <StateText state={tokenWord(t, now)} />
            <span className="text-sm">{expiryText(t, now)}</span>
          </>
        ) : (
          <span className="text-sm text-muted-foreground">Not read yet</span>
        )}
      </Field>
      <Field label="Checked">
        <span className="text-sm">{t?.checked_at ? ago(now - Date.parse(t.checked_at)) : ""}</span>
      </Field>
      <Field label="Rate limit">
        <span className="font-mono text-sm">{rateText(t)}</span>
      </Field>
      {t?.reason && t.state !== "ok" && <p className="py-3 text-sm text-destructive">{t.reason}</p>}
      <div className="py-3">
        <Button variant="outline" disabled={status.isError} onClick={() => setOpen(true)}>
          Replace token
        </Button>
      </div>
      <Dialog
        open={open}
        onOpenChange={(o) => {
          if (!o) setOpen(false)
        }}
      >
        {/* Mounted only while open: each opening starts clean, and a replace
            that ends after closing has nowhere to leave its error. */}
        <DialogContent>{open && <ReplaceTokenForm onClose={() => setOpen(false)} />}</DialogContent>
      </Dialog>
    </FieldGroup>
  )
}

function ReplaceTokenForm({ onClose }: { onClose: () => void }) {
  const queryClient = useQueryClient()
  const [text, setText] = useState("")
  const [error, setError] = useState("")
  const [busy, setBusy] = useState(false)
  const [confirm, setConfirm] = useState<Confirm | null>(null)

  async function send(value: string) {
    setBusy(true)
    try {
      await api.replaceToken(value)
      onClose()
      toast.success("GitHub token replaced")
      await queryClient.invalidateQueries({ queryKey: keys.token })
    } catch (err) {
      setError(errorText(err))
    } finally {
      setBusy(false)
    }
  }

  function ask() {
    if (text.trim() === "") return setError("Paste the new token first")
    setError("")
    const value = text
    setConfirm({ title: "Replace the GitHub token?", body: "ghr uses the new one at once.", action: "Replace", destructive: true, run: () => void send(value) })
  }

  return (
    <>
      <DialogHeader>
        <DialogTitle>Replace GitHub token</DialogTitle>
        <DialogDescription>
          Paste a fine-grained token with Administration: read/write and Actions: read on every configured repository.
        </DialogDescription>
      </DialogHeader>
      <DialogBody className="flex flex-col gap-2">
        <Label htmlFor="new-token">Token</Label>
        <Input id="new-token" type="password" autoComplete="off" value={text} onChange={(e) => setText(e.target.value)} />
        {error && <ErrorLine>{error}</ErrorLine>}
      </DialogBody>
      <DialogFooter>
        <Button variant="outline" onClick={onClose}>
          Cancel
        </Button>
        <Button loading={busy} onClick={ask}>
          Replace
        </Button>
      </DialogFooter>
      <ConfirmDialog confirm={confirm} onClose={() => setConfirm(null)} />
    </>
  )
}
