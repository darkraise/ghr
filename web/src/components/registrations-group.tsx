import { useMutation, useQueryClient } from "@tanstack/react-query"
import { Button } from "darkraise-ui/components/button"
import { toast } from "darkraise-ui/components/sonner"
import { Spinner } from "darkraise-ui/components/spinner"
import { useState, type ReactNode } from "react"
import { api } from "@/api/client"
import { keys, useRegistrations, useStatus } from "@/api/hooks"
import type { Registration } from "@/api/types"
import { ConfirmDialog, type Confirm } from "@/components/confirm-dialog"
import { ErrorLine } from "@/components/page/error-line"
import { FieldGroup } from "@/components/page/field"
import { StateText } from "@/components/page/state-text"
import { errorText } from "@/query"

export function RegistrationsGroup({
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
      toast.success(`Deleted ${r.name}`)
    },
    onSettled: () => queryClient.invalidateQueries({ queryKey: keys.registrations(name) }),
  })

  let body: ReactNode
  if (degraded) body = <p className="text-sm text-destructive">{`GitHub is rejecting the token: ${degradedReason ?? ""}`}</p>
  else if (regs.isError) body = <ErrorLine>{errorText(regs.error)}</ErrorLine>
  else if (!regs.data) body = <Spinner label="Loading" />
  else if (regs.data.length === 0) {
    body = <p className="text-sm text-muted-foreground">No runners registered. ghr starts single-use runners on demand, and keeps warm ones in all mode.</p>
  }

  return (
    <FieldGroup id="registrations" title="GitHub registrations">
      {body ? (
        <div className="py-3">{body}</div>
      ) : (
        (regs.data ?? []).map((r) => (
          <div key={r.id} className="flex flex-wrap items-center gap-x-3 gap-y-1 py-3 text-sm">
            <StateText state={r.busy ? "busy" : r.status} />
            {r.ghr && <span className="text-muted-foreground">ghr</span>}
            <span>{r.name}</span>
            <span className="font-mono text-xs text-muted-foreground">{r.labels.join(" ")}</span>
            {!r.ghr && !r.busy && r.status === "offline" && (
              <Button
                size="sm"
                variant="outline"
                className="ml-auto"
                aria-label={`Delete ${r.name}`}
                disabled={disabled}
                onClick={() =>
                  setConfirm({
                    title: `Delete runner registration ${r.name}?`,
                    body: `It is removed from ${name} on GitHub.`,
                    action: "Delete",
                    destructive: true,
                    run: () => remove.mutate(r),
                  })
                }
              >
                Delete
              </Button>
            )}
          </div>
        ))
      )}
      <div className="py-3">
        <Button size="sm" variant="outline" disabled={disabled || degraded} onClick={() => void regs.refetch()}>
          Refresh
        </Button>
      </div>
      <ConfirmDialog confirm={confirm} onClose={() => setConfirm(null)} />
    </FieldGroup>
  )
}
