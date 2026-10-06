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
import { useState } from "react"

export interface Confirm {
  title: string
  body?: string
  action: string
  destructive?: boolean
  run: () => void
}

export function ConfirmDialog({ confirm, onClose }: { confirm: Confirm | null; onClose: () => void }) {
  // The dialog animates closed after the caller drops its request; holding the
  // last one keeps the title from going blank in that moment.
  const [held, setHeld] = useState<Confirm | null>(confirm)
  if (confirm !== null && confirm !== held) setHeld(confirm)
  return (
    <AlertDialog
      open={confirm !== null}
      onOpenChange={(open) => {
        if (!open) onClose()
      }}
    >
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{held?.title}</AlertDialogTitle>
          {held?.body && <AlertDialogDescription>{held.body}</AlertDialogDescription>}
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>Cancel</AlertDialogCancel>
          <AlertDialogAction data-variant={held?.destructive ? "destructive" : "default"} onClick={() => held?.run()}>
            {held?.action}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}
