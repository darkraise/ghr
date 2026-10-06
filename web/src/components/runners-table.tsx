import { useMutation, useQueryClient } from "@tanstack/react-query"
import { Link, useNavigate } from "@tanstack/react-router"
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "darkraise-ui/components/alert-dialog"
import { Badge } from "darkraise-ui/components/badge"
import { Button } from "darkraise-ui/components/button"
import { toast } from "darkraise-ui/components/sonner"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "darkraise-ui/components/table"
import { useState } from "react"
import { ApiError, api } from "@/api/client"
import { keys, useStatus } from "@/api/hooks"
import type { InstanceStatus, Status } from "@/api/types"
import { StateBadge } from "@/components/state-badge"
import { copyText } from "@/lib/clipboard"
import { elapsed } from "@/lib/format"
import { useNow } from "@/lib/use-now"
import { errorText } from "@/query"

export function RunnersTable({ status, actions }: { status: Status; actions: boolean }) {
  const navigate = useNavigate()
  const now = useNow()
  const [stopping, setStopping] = useState<InstanceStatus | null>(null)
  const waiting = status.repos.filter((r) => !r.paused && r.queued > 0 && r.max > 0 && r.active >= r.max)

  if (status.instances.length === 0 && waiting.length === 0) {
    return <p className="py-6 text-center text-sm text-muted-foreground">no runners — they start when jobs are queued</p>
  }

  async function copy(id: string) {
    try {
      await copyText(id)
      toast.success(`copied ${id}`)
    } catch (err) {
      toast.error(errorText(err))
    }
  }

  return (
    <>
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>ID</TableHead>
            <TableHead>Repo</TableHead>
            <TableHead>State</TableHead>
            <TableHead>Job</TableHead>
            <TableHead>Elapsed</TableHead>
            {actions && <TableHead className="text-right">Actions</TableHead>}
          </TableRow>
        </TableHeader>
        <TableBody>
          {status.instances.map((i) => (
            <TableRow key={i.id}>
              <TableCell className="font-mono">
                <Link to="/runners/$id" params={{ id: i.id }} search={{ tab: "steps" }} className="hover:underline">
                  {i.id}
                </Link>
              </TableCell>
              <TableCell>{i.repo}</TableCell>
              <TableCell>
                <StateBadge state={i.state} />
              </TableCell>
              <TableCell>{i.job ? `${i.job.name} #${i.job.run_number}` : "–"}</TableCell>
              <TableCell>{elapsed(i, now)}</TableCell>
              {actions && (
                <TableCell>
                  <div className="flex justify-end gap-2">
                    <Button
                      size="sm"
                      variant="outline"
                      aria-label={`Logs for runner ${i.id}`}
                      onClick={() => void navigate({ to: "/runners/$id", params: { id: i.id }, search: { tab: "log" } })}
                    >
                      Logs
                    </Button>
                    <Button size="sm" variant="outline" aria-label={`Copy ID of runner ${i.id}`} onClick={() => void copy(i.id)}>
                      Copy ID
                    </Button>
                    <Button size="sm" variant="destructive" aria-label={`Stop runner ${i.id}`} onClick={() => setStopping(i)}>
                      Stop
                    </Button>
                  </div>
                </TableCell>
              )}
            </TableRow>
          ))}
          {waiting.map((r) => (
            <TableRow key={`waiting-${r.name}`}>
              <TableCell>–</TableCell>
              <TableCell>{r.name}</TableCell>
              <TableCell>
                <Badge variant="amber" size="sm">
                  ⧗ waiting
                </Badge>
              </TableCell>
              <TableCell colSpan={actions ? 3 : 2}>
                {r.queued} jobs queued (repo cap {r.max})
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
      <StopRunnerDialog instance={stopping} onClose={() => setStopping(null)} />
    </>
  )
}

export function StopRunnerDialog({ instance, onClose }: { instance: InstanceStatus | null; onClose: () => void }) {
  const queryClient = useQueryClient()
  const status = useStatus()
  // Holds the runner while the dialog animates closed, and reads its live
  // state so the wording follows a runner that picks up a job meanwhile.
  const [held, setHeld] = useState<InstanceStatus | null>(instance)
  if (instance !== null && instance.id !== held?.id) setHeld(instance)
  const current = held ? status.data?.instances.find((i) => i.id === held.id) : undefined
  const live = current ?? held
  // A runner can end while the dialog is open; there is then nothing to stop.
  // Not while closing: a runner just stopped here leaves /status meanwhile.
  const gone = instance !== null && status.data !== undefined && current === undefined
  const stop = useMutation({
    mutationFn: async (id: string) => {
      try {
        await api.stopRunner(id)
        return true
      } catch (err) {
        if (err instanceof ApiError && err.status === 404) return false
        throw err
      }
    },
    onSuccess: (stopped, id) => {
      if (stopped) toast.success(`stopped ${id}`)
      else toast.info(`${id} had already finished`)
    },
    onSettled: () => queryClient.invalidateQueries({ queryKey: keys.status }),
  })
  const busy = live?.state === "busy"
  let title = `Stop runner ${live?.id}?`
  let description = "ghr stops the runner and cleans it up."
  if (gone) {
    title = `Runner ${live?.id} has already finished`
    description = "There is nothing left to stop."
  } else if (busy) {
    title = `Runner ${live?.id} is running a job. Stop it?`
    description = "The job it is running fails."
  }
  return (
    <AlertDialog
      open={instance !== null}
      onOpenChange={(open) => {
        if (!open) onClose()
      }}
    >
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{title}</AlertDialogTitle>
          <AlertDialogDescription>{description}</AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>Cancel</AlertDialogCancel>
          <AlertDialogAction
            data-variant="destructive"
            disabled={gone}
            onClick={() => {
              if (live) stop.mutate(live.id)
            }}
          >
            Stop
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}
