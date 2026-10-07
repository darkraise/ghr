import { Badge, type BadgeVariant } from "darkraise-ui/components/badge"
import { useConfig, useStatus } from "@/api/hooks"
import { capText, running } from "@/lib/status"
import { useNow } from "@/lib/use-now"

const DAY = 86_400_000

function diskVariant(pct: number, highWater: number): BadgeVariant {
  if (pct >= 95) return "red"
  return pct >= highWater ? "amber" : "secondary"
}

export function StatusChips() {
  const status = useStatus()
  const config = useConfig()
  const now = useNow()
  const st = status.data
  if (!st) return null
  const highWater = config.data?.disk_high_water ?? 80
  const deadline = st.runner_update.deadline
  return (
    <div className="mb-4 flex flex-wrap gap-2">
      <Badge variant="outline">mode {st.mode.toUpperCase()}</Badge>
      <Badge variant="outline">
        runners {running(st)}/{capText(st)}
      </Badge>
      <Badge variant="outline">api {st.rate_remaining}</Badge>
      <Badge variant={diskVariant(st.disk_pct, highWater)}>disk {st.disk_pct}%</Badge>
      {st.degraded && <Badge variant="red">degraded: {st.degraded_reason}</Badge>}
      {deadline && <Badge variant={Date.parse(deadline) - now <= 7 * DAY ? "red" : "amber"}>runner ↑ {st.runner_update.latest}</Badge>}
    </div>
  )
}
