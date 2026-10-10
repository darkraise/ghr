import type { RepoStatus, Status } from "@/api/types"

export function running(status: Status): number {
  return status.instances.filter((i) => i.state !== "cleaning").length
}

export function repoStateWord(r: RepoStatus): "Error" | "Removing" | "Paused" | "Running" | "Idle" {
  if (r.error) return "Error"
  if (r.removing) return "Removing"
  if (r.paused) return "Paused"
  return r.active > 0 ? "Running" : "Idle"
}

export type Tone = "accent" | "run" | "warn" | "bad" | "ok" | "muted"

const tones: Record<string, Tone> = {
  busy: "run",
  running: "run",
  active: "run",
  in_progress: "run",
  online: "accent",
  matched: "accent",
  waiting: "warn",
  starting: "warn",
  queued: "warn",
  pending: "warn",
  requested: "warn",
  "expires soon": "warn",
  unverified: "warn",
  refused: "warn",
  interrupted: "warn",
  error: "bad",
  failed: "bad",
  failure: "bad",
  offline: "bad",
  unmatched: "bad",
  rejected: "bad",
  ok: "ok",
  success: "ok",
  succeeded: "ok",
  valid: "ok",
  "up to date": "ok",
}

export function stateTone(state: string): Tone {
  return tones[state.toLowerCase()] ?? "muted"
}

// One word per state, whatever the API calls it.
const words: Record<string, string> = {
  busy: "Running",
  in_progress: "Running",
  queued: "Waiting",
  pending: "Waiting",
  requested: "Waiting",
  success: "Succeeded",
  failure: "Failed",
}

export function stateWord(state: string): string {
  const known = words[state.toLowerCase()]
  if (known) return known
  const text = state.replace(/_/g, " ")
  return text.charAt(0).toUpperCase() + text.slice(1)
}

// A repository at its cap with jobs queued: the jobs wait on its own limit.
export function waitingRepos(status: Status): RepoStatus[] {
  return status.repos.filter((r) => !r.paused && r.queued > 0 && r.max > 0 && r.active >= r.max)
}
