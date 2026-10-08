import { Ban, Check, CircleSlash, X, type LucideIcon } from "lucide-react"

interface Result {
  label: string
  Icon: LucideIcon
  className: string
}

const FAILED: Result = { label: "Failed", Icon: X, className: "text-destructive" }

const RESULTS: Record<string, Result | undefined> = {
  success: { label: "Succeeded", Icon: Check, className: "text-success" },
  failure: FAILED,
  cancelled: { label: "Cancelled", Icon: Ban, className: "text-muted-foreground" },
  skipped: { label: "Skipped", Icon: CircleSlash, className: "text-muted-foreground" },
}

// Any other conclusion shows as failed, as the Dashboard has always shown it.
export function ResultIcon({ conclusion }: { conclusion: string }) {
  const { label, Icon, className } = RESULTS[conclusion] ?? FAILED
  return <Icon role="img" aria-label={label} size={15} className={`shrink-0 ${className}`} />
}
