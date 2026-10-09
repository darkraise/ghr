import { useMutation, useQueryClient } from "@tanstack/react-query"
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
import { toast } from "darkraise-ui/components/sonner"
import { useState } from "react"
import { ApiError, api } from "@/api/client"
import { keys, useStatus } from "@/api/hooks"
import type { InstanceStatus } from "@/api/types"

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
      if (stopped) toast.success(`Stopped ${id}`)
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
