import { useMutation, useQueryClient } from "@tanstack/react-query"
import { Badge } from "darkraise-ui/components/badge"
import { Button } from "darkraise-ui/components/button"
import { Card, CardContent, CardHeader, CardTitle } from "darkraise-ui/components/card"
import { toast } from "darkraise-ui/components/sonner"
import { useState } from "react"
import { api } from "@/api/client"
import { keys, useRegistrations, useStatus } from "@/api/hooks"
import type { Registration } from "@/api/types"
import { ConfirmDialog, type Confirm } from "@/components/confirm-dialog"
import { StateBadge } from "@/components/state-badge"
import { errorText } from "@/query"

export function RegistrationsCard({
  name,
  degraded,
  degradedReason,
  disabled,
}: {
  name: string
  degraded: boolean
  degradedReason?: string
  disabled: boolean
}) {
  const statusRead = useStatus().data !== undefined
  const regs = useRegistrations(name, statusRead && !degraded)
  const queryClient = useQueryClient()
  const [confirm, setConfirm] = useState<Confirm | null>(null)
  const remove = useMutation({
    mutationFn: (r: Registration) => api.deleteRegistration(name, r.id),
    onSuccess: (_data, r) => {
      toast.success(`deleted ${r.name}`)
    },
    onSettled: () => queryClient.invalidateQueries({ queryKey: keys.registrations(name) }),
  })

  let body
  if (degraded) body = <p className="text-destructive">GitHub is rejecting the token: {degradedReason}</p>
  else if (regs.isError) body = <p className="text-destructive">✖ {errorText(regs.error)}</p>
  else if (!regs.data) body = <p className="text-muted-foreground">loading…</p>
  else if (regs.data.length === 0) {
    body = (
      <p className="text-muted-foreground">
        No runners registered. ghr starts single-use runners on demand (and keeps warm ones in all mode).
      </p>
    )
  } else {
    body = (
      <ul className="flex flex-col gap-2">
        {regs.data.map((r) => (
          <li key={r.id} className="flex flex-wrap items-center gap-2">
            <StateBadge state={r.busy ? "busy" : r.status} />
            {r.ghr && (
              <Badge variant="secondary" size="sm">
                ghr
              </Badge>
            )}
            <span>{r.name}</span>
            <span className="text-muted-foreground">{r.labels.join(" ")}</span>
            {!r.ghr && !r.busy && r.status === "offline" && (
              <Button
                size="sm"
                variant="destructive"
                aria-label={`Delete ${r.name}`}
                disabled={disabled}
                onClick={() =>
                  setConfirm({
                    title: `Delete the runner registration ${r.name} from ${name}?`,
                    action: "Delete",
                    destructive: true,
                    run: () => remove.mutate(r),
                  })
                }
              >
                Delete
              </Button>
            )}
          </li>
        ))}
      </ul>
    )
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>GitHub registrations</CardTitle>
      </CardHeader>
      <CardContent className="flex flex-col gap-3 text-sm">
        {body}
        <div>
          <Button size="sm" variant="outline" disabled={disabled || degraded} onClick={() => void regs.refetch()}>
            Refresh
          </Button>
        </div>
      </CardContent>
      <ConfirmDialog confirm={confirm} onClose={() => setConfirm(null)} />
    </Card>
  )
}
