import { Spinner } from "darkraise-ui/components/spinner"
import { Clock } from "lucide-react"
import { ResultIcon } from "@/components/result-icon"
import { isRunActive } from "@/lib/run-details"

export function RunStatusIcon({ status, conclusion }: { status: string; conclusion: string }) {
  if (!isRunActive(status)) return <ResultIcon conclusion={conclusion} />
  if (status === "in_progress") return <Spinner size="sm" label={<span className="sr-only">Running</span>} />
  return <Clock role="img" aria-label="Queued" size={15} className="shrink-0 text-muted-foreground" />
}
