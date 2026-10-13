import type { ActivityWeek, RepoStatus, Status } from "@/api/types"
import type { Confirm } from "@/components/confirm-dialog"
import { plural } from "./format"

export const PUBLIC_HELP = "A job in a public repository can come from anyone's pull request and runs on this host."

export function servePublicConfirm(name: string, run: () => void): Confirm {
  return { title: `Serve ${name} while it is public?`, body: PUBLIC_HELP, action: "Serve when public", run }
}

// Cancelled, skipped and unknown runs say nothing about health, so the rate
// leaves them out.
export function weekRate(week: ActivityWeek | undefined): number | undefined {
  if (!week) return undefined
  const decided = week.succeeded + week.failed
  return decided === 0 ? undefined : Math.round((week.succeeded / decided) * 100)
}

export function weekText(week: ActivityWeek): string {
  const unknown = week.unknown > 0 ? `, ${week.unknown} unknown` : ""
  return `${week.succeeded} succeeded, ${week.failed} failed, ${week.cancelled} cancelled${unknown} in 7 days`
}

// A repository being removed is on its way out, as the Dashboard counts it.
export function reposSummary(status: Status): string {
  const live = status.repos.filter((r) => !r.removing)
  const counts: [number, string][] = [
    [live.length, "configured"],
    [live.filter((r) => r.active > 0).length, "running"],
    [live.filter((r) => r.paused).length, "paused"],
  ]
  return counts
    .filter(([n]) => n > 0)
    .map(([n, word]) => `${n} ${word}`)
    .join(", ")
}

export function runnersText(repo: RepoStatus): string {
  return repo.max === 0 ? `${repo.active}, no limit` : `${repo.active} of ${repo.max}`
}

export function repoDescription(repo: RepoStatus): string {
  if (repo.error) return repo.error
  if (repo.paused) return "Paused"
  const waiting = repo.queued > 0 ? `, ${plural(repo.queued, "job")} waiting` : ""
  if (repo.active === 0) return `Idle${waiting}`
  const runners = repo.max === 0 ? plural(repo.active, "runner") : `${repo.active} of ${repo.max} runners`
  return `Running ${runners}${waiting}`
}
