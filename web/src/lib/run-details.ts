import type { Annotation, RunDetail, RunJob, Step } from "@/api/types"
import { resultWord } from "@/lib/history"

export interface RunSearch {
  attempt?: number
}

export interface JobSearch {
  attempt?: number
  step?: number
  line?: number
}

export function positiveInt(v: unknown): number | undefined {
  const n = typeof v === "number" ? v : typeof v === "string" && /^\d+$/.test(v) ? Number(v) : NaN
  return Number.isSafeInteger(n) && n > 0 ? n : undefined
}

function pick<K extends string>(search: Record<string, unknown>, names: K[]): Partial<Record<K, number>> {
  const out: Partial<Record<K, number>> = {}
  for (const name of names) {
    const n = positiveInt(search[name])
    if (n !== undefined) out[name] = n
  }
  return out
}

export function parseRunSearch(search: Record<string, unknown>): RunSearch {
  return pick(search, ["attempt"])
}

export function parseJobSearch(search: Record<string, unknown>): JobSearch {
  return pick(search, ["attempt", "step", "line"])
}

export const isRunActive = (status: string): boolean => status !== "completed"

export function statusText(status: string, conclusion: string): string {
  if (!isRunActive(status)) return resultWord(conclusion)
  return status === "in_progress" ? "Running" : "Queued"
}

export function jobMs(job: RunJob, now: number): number | undefined {
  if (!job.started_at) return undefined
  const end = job.completed_at ? Date.parse(job.completed_at) : now
  return end - Date.parse(job.started_at)
}

export function runTotalMs(run: RunDetail, now: number): number {
  const start = Date.parse(run.started_at)
  if (isRunActive(run.status)) return now - start
  let end = start
  for (const j of run.jobs) if (j.completed_at) end = Math.max(end, Date.parse(j.completed_at))
  return end - start
}

export function runDescription(run: RunDetail): string {
  const where = `${run.workflow} #${run.run_number} in ${run.repo}`
  return run.branch ? `${where} on ${run.branch}.` : `${where}.`
}

// A run's path can carry the ref it was run from, as in
// ".github/workflows/ci.yml@refs/heads/main"; the daemon strips it the same way.
const PATH_REF = /^(\.github\/workflows\/[^@]+\.ya?ml)@.+$/

export function githubLinks(owner: string, run: RunDetail): { commit: string; workflowFile: string | undefined; pull: (n: number) => string } {
  const base = `https://github.com/${owner}/${run.repo}`
  const path = PATH_REF.exec(run.path)?.[1] ?? run.path
  return {
    commit: `${base}/commit/${run.head_sha}`,
    workflowFile: path.startsWith(".github/workflows/") ? `${base}/blob/${run.head_sha}/${path.split("/").map(encodeURIComponent).join("/")}` : undefined,
    pull: (n) => `${base}/pull/${n}`,
  }
}

export function attemptLabel(n: number, attempts: number): string {
  return n === attempts ? `Attempt ${n} (latest)` : `Attempt ${n}`
}

export interface AnnotationCounts {
  error: number
  warning: number
  notice: number
}

export function annotationCounts(items: Annotation[]): AnnotationCounts {
  const out: AnnotationCounts = { error: 0, warning: 0, notice: 0 }
  for (const a of items) if (a.level === "error" || a.level === "warning" || a.level === "notice") out[a.level]++
  return out
}

export function firstFailedStep(steps: Step[]): number | undefined {
  return steps.find((s) => s.conclusion === "failure")?.number
}

export function rawLogFileName(repo: string, jobId: number): string {
  return `${repo}-job-${jobId}.log`
}
