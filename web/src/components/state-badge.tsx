import { Badge } from "darkraise-ui/components/badge"
import { stateVariant } from "@/lib/status"

export function StateBadge({ state }: { state: string }) {
  return (
    <Badge variant={stateVariant(state)} size="sm">
      {state}
    </Badge>
  )
}
