import type { BadgeVariant } from "darkraise-ui/components/badge"
import type { RepoStatus, Status } from "@/api/types"

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

export function repoState(r: RepoStatus): string {
  if (r.error) return "error"
  if (r.removing) return "removing"
  if (r.paused) return "paused"
  return "active"
}

export function running(status: Status): number {
  return status.instances.filter((i) => i.state !== "cleaning").length
}

export function repoStateWord(r: RepoStatus): "Error" | "Removing" | "Paused" | "Running" | "Idle" {
  if (r.error) return "Error"
  if (r.removing) return "Removing"
  if (r.paused) return "Paused"
  return r.active > 0 ? "Running" : "Idle"
}
