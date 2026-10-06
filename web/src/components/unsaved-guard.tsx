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

export function UnsavedGuard({
  count,
  page,
  saving,
  onSave,
  onDiscard,
}: {
  count: number
  page: string
  saving: boolean
  onSave: () => Promise<boolean>
  onDiscard: () => void
}) {
  const blocker = useBlocker({ shouldBlockFn: () => count > 0 || saving, enableBeforeUnload: count > 0, withResolver: true })
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
          <Button disabled={saving} onClick={() => void onSave().then((ok) => (ok ? proceed() : reset()))}>
            Save
          </Button>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}
