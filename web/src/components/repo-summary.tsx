import { Button } from "darkraise-ui/components/button"
import { ACTIVITY_LIMIT, useRepoActivity } from "@/api/hooks"
import type { RepoStatus } from "@/api/types"
import { StateBadge } from "@/components/state-badge"
import { summarize } from "@/lib/activity"
import { ago, maxText } from "@/lib/format"
import { errorText } from "@/query"

export function repoState(r: RepoStatus): string {
  if (r.error) return "error"
  if (r.removing) return "removing"
  if (r.paused) return "paused"
  return "active"
}

const outcome = (conclusion: string) => (conclusion === "success" ? "succeeded" : conclusion === "cancelled" ? "cancelled" : "failed")

export function RepoSummary({ repo, now }: { repo: RepoStatus; now: number }) {
  const job = repo.last_job
  const ok = job?.conclusion === "success"
  return (
    <div className="flex flex-col gap-1 text-sm">
      <div className="flex items-center gap-2">
        <StateBadge state={repoState(repo)} />
        <span>
          {repo.active}/{maxText(repo.max)} running · {repo.queued} queued
        </span>
      </div>
      {job ? (
        <p>
          <span role="img" aria-label={outcome(job.conclusion)} className={ok ? "text-green-600" : "text-destructive"}>
            {ok ? "✔" : "✖"}
          </span>{" "}
          #{job.run_number} {job.job_name} · {ago(now - Date.parse(job.finished_at))}
        </p>
      ) : (
        <p className="text-muted-foreground">no finished jobs yet</p>
      )}
      {repo.removing && <p className="text-amber-600">removing… running jobs finish first</p>}
      {repo.error && <p className="text-destructive">{repo.error}</p>}
    </div>
  )
}

export function ActivitySummary({ name, retention, now }: { name: string; retention: string | undefined; now: number }) {
  const activity = useRepoActivity(name)
  if (activity.isError) {
    return (
      <div className="flex items-center gap-2 text-sm">
        <span className="text-destructive">✖ {errorText(activity.error)}</span>
        <Button size="sm" variant="outline" onClick={() => void activity.refetch()}>
          Retry
        </Button>
      </div>
    )
  }
  if (!activity.data) return <p className="text-sm text-muted-foreground">loading…</p>
  const a = summarize(activity.data, now, retention, ACTIVITY_LIMIT)
  return (
    <div className="flex flex-col gap-1 text-sm">
      <p>
        {a.line}
        <span className="text-muted-foreground"> · {a.label}</span>
      </p>
      {a.strip.length === 0 ? (
        <p className="text-muted-foreground">no finished jobs yet</p>
      ) : (
        <p className="font-mono tracking-wider">
          {a.strip.map((m) => (
            <span key={m.id} role="img" aria-label={m.label} className={m.className}>
              {m.symbol}
            </span>
          ))}
        </p>
      )}
    </div>
  )
}
