import { useQueryClient } from "@tanstack/react-query"
import { Button } from "darkraise-ui/components/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "darkraise-ui/components/card"
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
import { useState, type ReactNode } from "react"
import { api } from "@/api/client"
import { keys, useStatus, useToken } from "@/api/hooks"
import type { TokenStatus } from "@/api/types"
import { ConfirmDialog, type Confirm } from "@/components/confirm-dialog"
import { StateBadge } from "@/components/state-badge"
import { DAY_MS } from "@/lib/duration"
import { ago, dateTime, hhmm } from "@/lib/format"
import { useNow } from "@/lib/use-now"
import { errorText } from "@/query"

function badge(t: TokenStatus, now: number): string {
  if (t.state === "ok" && t.expires_at && Date.parse(t.expires_at) - now < 14 * DAY_MS) return "expires soon"
  return t.state
}

function expiry(t: TokenStatus, now: number): string {
  if (!t.expires_at) return "expiry unknown"
  return `expires ${dateTime(t.expires_at).slice(0, 10)} (${Math.floor((Date.parse(t.expires_at) - now) / DAY_MS)} days)`
}

function rate(t: TokenStatus | undefined): string {
  if (t?.rate_remaining === undefined) return "–"
  const limit = t.rate_limit !== undefined ? ` / ${t.rate_limit}` : ""
  const reset = t.rate_reset ? `, resets ${hhmm(new Date(t.rate_reset))}` : ""
  return `${t.rate_remaining}${limit}${reset}`
}

function Row({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="flex flex-wrap items-center gap-2 text-sm">
      <span className="w-24 text-muted-foreground">{label}</span>
      {children}
    </div>
  )
}

export function TokenCard() {
  const token = useToken()
  const status = useStatus()
  const queryClient = useQueryClient()
  const now = useNow()
  const [open, setOpen] = useState(false)
  const [text, setText] = useState("")
  const [error, setError] = useState("")
  const [busy, setBusy] = useState(false)
  const [confirm, setConfirm] = useState<Confirm | null>(null)
  const t = token.data

  function close() {
    setOpen(false)
    setText("")
    setError("")
  }

  async function send(value: string) {
    setBusy(true)
    try {
      await api.replaceToken(value)
      close()
      toast.success("GitHub token replaced")
      await queryClient.invalidateQueries({ queryKey: keys.token })
    } catch (err) {
      setError(`✖ ${errorText(err)}`)
    } finally {
      setBusy(false)
    }
  }

  function ask() {
    if (text.trim() === "") return setError("paste the new token first")
    setError("")
    const value = text
    setConfirm({
      title: "Replace the GitHub token? ghr uses the new one at once.",
      action: "Replace",
      destructive: true,
      run: () => void send(value),
    })
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>GitHub token</CardTitle>
        <CardDescription>
          Replacing checks read access to the first repository only; registration permissions are checked when a runner is next started.
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-2">
        {token.isError && (
          <p className="text-sm text-destructive">
            ✖ {errorText(token.error)}{" "}
            <Button variant="link" className="px-1" onClick={() => void token.refetch()}>
              Retry
            </Button>
          </p>
        )}
        <Row label="Status">
          {t ? (
            <>
              <StateBadge state={badge(t, now)} />
              <span>{expiry(t, now)}</span>
              {t.checked_at && <span className="text-muted-foreground">· checked {ago(now - Date.parse(t.checked_at))}</span>}
            </>
          ) : (
            <span className="text-muted-foreground">not read yet</span>
          )}
        </Row>
        <Row label="Rate limit">
          <span>{rate(t)}</span>
        </Row>
        {t?.reason && <p className="text-sm text-destructive">{t.reason}</p>}
        <div>
          <Button disabled={status.isError} onClick={() => setOpen(true)}>
            Replace token
          </Button>
        </div>
      </CardContent>
      <Dialog
        open={open}
        onOpenChange={(o) => {
          if (!o) close()
        }}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Replace GitHub token</DialogTitle>
            <DialogDescription>
              Paste a fine-grained token with Administration: read/write and Actions: read on every configured repository.
            </DialogDescription>
          </DialogHeader>
          <DialogBody className="flex flex-col gap-2">
            <Label htmlFor="new-token">Token</Label>
            <Input id="new-token" type="password" autoComplete="off" value={text} onChange={(e) => setText(e.target.value)} />
            {error && <p className="text-sm text-destructive">{error}</p>}
          </DialogBody>
          <DialogFooter>
            <Button variant="secondary" onClick={close}>
              Cancel
            </Button>
            <Button loading={busy} onClick={ask}>
              Replace
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
      <ConfirmDialog confirm={confirm} onClose={() => setConfirm(null)} />
    </Card>
  )
}
