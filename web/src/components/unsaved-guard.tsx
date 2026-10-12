import { useBlocker } from "@tanstack/react-router"
import {
  AlertDialog,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "darkraise-ui/components/alert-dialog"
import { Button } from "darkraise-ui/components/button"
import { useEffect, useRef, useState } from "react"
import { setUnsavedCount } from "@/lib/unsaved"

export function UnsavedGuard({
  count,
  page,
  saving,
  allow,
  onSave,
  onDiscard,
}: {
  count: number
  page: string
  saving: boolean
  // Pathnames that keep the form and its draft mounted, so moving there loses nothing.
  allow?: (pathname: string) => boolean
  onSave: () => Promise<boolean>
  onDiscard: () => void
}) {
  // A navigation inside the page (a hash change) never leaves the form, and
  // blocking one makes the router undo it with a reload.
  const blocker = useBlocker({
    shouldBlockFn: ({ current, next }) => (count > 0 || saving) && next.pathname !== current.pathname && !allow?.(next.pathname),
    enableBeforeUnload: count > 0,
    withResolver: true,
  })
  useEffect(() => {
    setUnsavedCount(count)
    return () => setUnsavedCount(0)
  }, [count])
  const [saved, setSaved] = useState(false)
  // A save started from one prompt must not act on a later one.
  const prompt = useRef(0)
  useEffect(() => {
    if (blocker.status === "blocked") return
    setSaved(false)
    prompt.current++
  }, [blocker.status])
  // Leaves only once the save has settled every change: an edit the save did
  // not carry keeps the page, with the save bar showing what is left.
  useEffect(() => {
    if (!saved || saving || blocker.status !== "blocked") return
    setSaved(false)
    if (count === 0) blocker.proceed()
    else blocker.reset()
  }, [saved, saving, count, blocker])
  if (blocker.status !== "blocked") return null
  const { proceed, reset } = blocker
  const changes = count === 1 ? "1 unsaved change" : `${count} unsaved changes`
  return (
    <AlertDialog
      open
      onOpenChange={(open) => {
        if (!open) reset()
      }}
    >
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>Unsaved changes</AlertDialogTitle>
          <AlertDialogDescription>{saving ? "wait for the save to finish" : `You have ${changes} on the ${page} page.`}</AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <Button variant="secondary" onClick={reset}>
            Stay
          </Button>
          <Button
            variant="secondary"
            disabled={saving}
            onClick={() => {
              onDiscard()
              proceed()
            }}
          >
            Discard
          </Button>
          <Button
            disabled={saving || saved}
            onClick={() => {
              const started = prompt.current
              void onSave().then((ok) => {
                if (started !== prompt.current) return
                if (ok) setSaved(true)
                else reset()
              })
            }}
          >
            Save
          </Button>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}
