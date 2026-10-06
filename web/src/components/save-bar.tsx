import { Alert, AlertDescription, AlertTitle } from "darkraise-ui/components/alert"
import { Button } from "darkraise-ui/components/button"
import { rejected, unsavedText } from "@/lib/draft"

export function SaveBar({
  count,
  saving,
  disabled,
  onSave,
  onDiscard,
}: {
  count: number
  saving: boolean
  disabled?: boolean
  onSave: () => void
  onDiscard: () => void
}) {
  if (count === 0) return null
  return (
    <div className="sticky bottom-0 z-10 mt-4 flex items-center justify-between gap-4 rounded-md border bg-background p-3 shadow">
      <span className="text-sm text-amber-600">{unsavedText(count)}</span>
      <div className="flex gap-2">
        <Button variant="secondary" disabled={saving || disabled} onClick={onDiscard}>
          Discard
        </Button>
        <Button disabled={saving || disabled} onClick={onSave}>
          {saving ? "Saving…" : "Save changes"}
        </Button>
      </div>
    </div>
  )
}

export function RejectedAlert({ message }: { message: string }) {
  const { lines, more } = rejected(message)
  return (
    <Alert variant="destructive" className="mb-4">
      <AlertTitle>Save rejected</AlertTitle>
      <AlertDescription>
        <ul>
          {lines.map((line) => (
            <li key={line}>✖ {line}</li>
          ))}
          {more > 0 && <li>… and {more} more</li>}
        </ul>
      </AlertDescription>
    </Alert>
  )
}
