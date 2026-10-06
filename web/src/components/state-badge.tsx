import { Badge, type BadgeVariant } from "darkraise-ui/components/badge"

const variants: Record<string, BadgeVariant> = {
  active: "green",
  online: "green",
  success: "green",
  matched: "green",
  ok: "green",
  busy: "blue",
  paused: "amber",
  idle: "amber",
  starting: "amber",
  "expires soon": "amber",
  unverified: "amber",
  error: "red",
  failure: "red",
  offline: "red",
  unmatched: "red",
  rejected: "red",
}

export function stateVariant(state: string): BadgeVariant {
  return variants[state] ?? "secondary"
}

export function StateBadge({ state }: { state: string }) {
  return (
    <Badge variant={stateVariant(state)} size="sm">
      {state}
    </Badge>
  )
}
