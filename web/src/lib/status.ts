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

export type Tone = "accent" | "warn" | "bad" | "ok" | "muted"

const tones: Record<string, Tone> = {
  busy: "accent",
  running: "accent",
  active: "accent",
  online: "accent",
  matched: "accent",
  waiting: "warn",
  starting: "warn",
  queued: "warn",
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
  valid: "ok",
  "up to date": "ok",
}

export function stateTone(state: string): Tone {
  return tones[state.toLowerCase()] ?? "muted"
}

// A repository at its cap with jobs queued: the jobs wait on its own limit.
export function waitingRepos(status: Status): RepoStatus[] {
  return status.repos.filter((r) => !r.paused && r.queued > 0 && r.max > 0 && r.active >= r.max)
}
