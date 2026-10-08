import { Button } from "darkraise-ui/components/button"
import { CircleAlert } from "lucide-react"
import type { ReactNode } from "react"

export function ErrorLine({ children, onRetry }: { children: ReactNode; onRetry?: () => void }) {
  return (
    <div role="alert" className="flex flex-wrap items-center gap-2 text-sm text-destructive">
      <CircleAlert size={15} aria-hidden="true" className="shrink-0" />
      <span>{children}</span>
      {onRetry && (
        <Button size="sm" variant="outline" onClick={onRetry}>
          Try again
        </Button>
      )}
    </div>
  )
}
