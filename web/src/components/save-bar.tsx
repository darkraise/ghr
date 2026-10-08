import { Alert, AlertDescription, AlertTitle } from "darkraise-ui/components/alert"
import { Button } from "darkraise-ui/components/button"
import { CircleAlert } from "lucide-react"
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
    <div className="sticky bottom-0 z-10 mt-4 flex flex-wrap items-center justify-between gap-3 rounded-[10px] border border-border bg-card p-3">
      <span className="text-sm text-warning">{unsavedText(count)}</span>
      <div className="flex gap-2">
        <Button variant="outline" disabled={saving || disabled} onClick={onDiscard}>
          Discard
        </Button>
        <Button loading={saving} disabled={saving || disabled} onClick={onSave}>
          Save changes
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
        <ul className="flex flex-col gap-1">
          {lines.map((line) => (
            <li key={line} className="flex items-center gap-1.5">
              <CircleAlert size={13} aria-hidden="true" className="shrink-0" />
              <span>{line}</span>
            </li>
          ))}
          {more > 0 && <li>{`and ${more} more`}</li>}
        </ul>
      </AlertDescription>
    </Alert>
  )
}
