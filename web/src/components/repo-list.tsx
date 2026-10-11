import { Link } from "@tanstack/react-router"
import type { ActivityRepo, Config, RepoStatus } from "@/api/types"
import { StatusPill } from "@/components/page/status-pill"
import { RepoActivityStrip } from "@/components/repo-activity-strip"
import { RepoRowMenu } from "@/components/repo-row-menu"
import { RunnerMeter } from "@/components/runner-meter"
import { weekRate, weekText } from "@/lib/repos"
import { repoStateWord } from "@/lib/status"

const LABEL = "rounded-full bg-muted px-2 py-0.5 font-mono text-xs text-foreground"

export function RepoList({
  repos,
  config,
  activity,
  offline,
  onEdit,
}: {
  repos: RepoStatus[]
  config: Config | undefined
  activity: ActivityRepo[] | undefined
  offline: boolean
  onEdit: (name: string) => void
}) {
  return (
    <ul className="flex flex-col divide-y divide-border">
      {repos.map((r) => {
        const seen = activity?.find((a) => a.repo.toLowerCase() === r.name.toLowerCase())
        const cfg = config?.repos?.find((c) => c.name === r.name)
        const rate = weekRate(seen?.week)
        return (
          <li key={r.name} className="flex flex-col gap-2 px-4 py-3 hover:bg-muted/50 md:flex-row md:items-start md:gap-6">
            <div className="flex min-w-0 flex-1 flex-col gap-1.5">
              <span className="flex flex-wrap items-center gap-2">
                <Link to="/repositories/$name" params={{ name: r.name }} className="text-sm font-semibold break-all text-primary hover:underline">
                  {r.name}
                </Link>
                <StatusPill state={repoStateWord(r).toLowerCase()} />
              </span>
              {r.error && <p className="text-xs text-destructive">{r.error}</p>}
              {r.removing ? (
                <p className="text-xs text-muted-foreground">Removing. Running jobs finish first.</p>
              ) : (
                <span className="flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-muted-foreground">
                  {(cfg?.labels ?? []).map((l) => (
                    <span key={l} className={LABEL}>
                      {l}
                    </span>
                  ))}
                  {config?.mode === "all" && cfg && <span>{`Warm ${cfg.warm ?? 1}`}</span>}
                  {rate !== undefined && seen && (
                    <span>
                      <span className="font-mono">{`${rate}%`}</span> succeeded in 7 days
                      <span className="sr-only">{` ${weekText(seen.week)}`}</span>
                    </span>
                  )}
                </span>
              )}
            </div>
            <div className="flex w-full flex-wrap items-center gap-4 md:w-auto md:flex-nowrap">
              <span className="flex items-center gap-2 font-mono text-sm">
                {r.max === 0 ? r.active : `${r.active}/${r.max}`}
                {r.max > 0 && <RunnerMeter active={r.active} max={r.max} />}
              </span>
              {r.queued > 0 && <span className="font-mono text-sm text-warning">{`${r.queued} waiting`}</span>}
              {seen && (
                <span className="hidden md:block">
                  <RepoActivityStrip repo={r.name} hours={seen.hours} />
                </span>
              )}
              <span className="ml-auto md:ml-0">
                <RepoRowMenu repo={r} offline={offline} onEdit={onEdit} />
              </span>
            </div>
          </li>
        )
      })}
    </ul>
  )
}
