import type { RepoStatus, Status } from "@/api/types"
import { plural } from "./format"
import { running } from "./status"

function where(names: string[]): string {
  if (names.length === 1) return `in ${names[0] ?? ""}`
  if (names.length === 2) return `in ${names[0] ?? ""} and ${names[1] ?? ""}`
  return `across ${names.length} repositories`
}

export function allPaused(repos: RepoStatus[]): boolean {
  const live = repos.filter((r) => !r.removing)
  return live.length > 0 && live.every((r) => r.paused)
}

// A degraded daemon has its own banner, so the sentence leaves it out.
export function dashboardSummary(status: Status): string {
  const busy = status.instances.filter((i) => i.state === "busy").length
  const parts: string[] = []
  if (allPaused(status.repos)) {
    parts.push(
      busy === 0
        ? "All repositories are paused."
        : `All repositories are paused, and ${plural(busy, "runner")} ${busy === 1 ? "is" : "are"} finishing.`,
    )
  } else if (busy === 0) {
    parts.push("No runners busy.")
  } else {
    parts.push(status.mode === "all" ? `${plural(busy, "runner")} busy.` : `${busy} of ${status.global_max} runners busy.`)
  }
  const waiting = status.repos.filter((r) => r.queued > 0)
  const queued = waiting.reduce((n, r) => n + r.queued, 0)
  if (queued > 0) parts.push(`${queued === 1 ? "1 job is" : `${queued} jobs are`} waiting ${where(waiting.map((r) => r.name))}.`)
  const errors = status.repos.filter((r) => r.error)
  const first = errors[0]
  if (errors.length === 1 && first) parts.push(`${first.name}: ${(first.error ?? "").replace(/\.$/, "")}.`)
  else if (errors.length > 1) parts.push(`${errors.length} repositories have errors.`)
  return parts.join(" ")
}

export function runnersSummary(status: Status): string {
  const live = running(status)
  const queued = status.repos.reduce((n, r) => n + r.queued, 0)
  const head = live === 0 ? "No live runners" : `${live} live`
  return queued > 0 ? `${head}, ${plural(queued, "job")} waiting` : head
}
