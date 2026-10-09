import type { ActionsRepo, ActionsRun } from "@/api/types"
import { hhmm, monthDay } from "./format"
import { resultWord } from "./history"

export const STATUS_FILTERS = [
  { value: "active", label: "Active" },
  { value: "failed", label: "Failed" },
  { value: "succeeded", label: "Succeeded" },
] as const
export type ActionsStatus = (typeof STATUS_FILTERS)[number]["value"]

export interface ActionsSearch {
  repo?: string
  status?: ActionsStatus
}

// Unknown values are dropped, so the page falls back to its defaults; the
// page also drops a repository that is neither configured nor watched.
export function parseActionsSearch(search: Record<string, unknown>): ActionsSearch {
  const out: ActionsSearch = {}
  if (typeof search.repo === "string" && search.repo !== "") out.repo = search.repo
  const status = STATUS_FILTERS.find((s) => s.value === search.status)
  if (status) out.status = status.value
  return out
}

export const isActive = (r: ActionsRun): boolean => r.status !== "completed"
// Only in_progress is running; queued, waiting, pending, requested and any
// status GitHub adds later wait.
export const isQueued = (r: ActionsRun): boolean => isActive(r) && r.status !== "in_progress"

export function statusWord(r: ActionsRun): string {
  if (!isActive(r)) return resultWord(r.conclusion)
  return isQueued(r) ? "Queued" : "Running"
}

function matchesRepo(r: ActionsRun, repo: string | undefined): boolean {
  return !repo || r.repo.toLowerCase() === repo.toLowerCase()
}

// Failed and Succeeded follow resultWord, so the filter agrees with the icon.
function matchesStatus(r: ActionsRun, status: ActionsStatus | undefined): boolean {
  if (!status) return true
  if (status === "active") return isActive(r)
  if (isActive(r)) return false
  return resultWord(r.conclusion) === (status === "failed" ? "Failed" : "Succeeded")
}

export const RECENT_LIMIT = 100

// The filter applies before the cap, so Failed still finds failures that
// sit behind 100 successes.
export function splitRuns(
  runs: ActionsRun[],
  repo: string | undefined,
  status: ActionsStatus | undefined,
): { active: ActionsRun[]; recent: ActionsRun[] } {
  const kept = runs.filter((r) => matchesRepo(r, repo) && matchesStatus(r, status))
  return { active: kept.filter(isActive), recent: kept.filter((r) => !isActive(r)).slice(0, RECENT_LIMIT) }
}

export function runMs(r: ActionsRun, now: number): number {
  const end = isActive(r) ? now : Date.parse(r.updated_at)
  return Math.max(0, end - Date.parse(r.started_at))
}

export function startText(iso: string, now: number): string {
  const d = new Date(iso)
  const today = new Date(now)
  const sameDay = d.getFullYear() === today.getFullYear() && d.getMonth() === today.getMonth() && d.getDate() === today.getDate()
  return sameDay ? hhmm(d) : `${monthDay(d)} ${hhmm(d)}`
}

export function actionsSummary(runs: ActionsRun[], repos: number, repo: string | undefined, fetchedAt: Date): string {
  const shown = runs.filter((r) => matchesRepo(r, repo))
  const running = shown.filter((r) => r.status === "in_progress").length
  const queued = shown.filter(isQueued).length
  const where = repo ? `in ${repo}` : `across ${repos} ${repos === 1 ? "repository" : "repositories"}`
  return `${running} running, ${queued} queued ${where}. Updated ${hhmm(fetchedAt)}.`
}

export function repoErrorText(r: ActionsRepo): string {
  const text = `${r.repo}: ${r.error ?? ""}`
  return r.retry_at ? `${text.replace(/\.$/, "")}. Retry after ${hhmm(new Date(r.retry_at))}` : text
}
