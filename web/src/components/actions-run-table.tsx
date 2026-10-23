import { Link } from "@tanstack/react-router"
import type { ActionsRun } from "@/api/types"
import { RepoFact, RunGroup, RunRow, RunValue } from "@/components/page/run-row"
import { RunStatusIcon } from "@/components/run-status-icon"
import { runMs, startText } from "@/lib/actions"
import { dur } from "@/lib/format"
import { groupByDayOf } from "@/lib/history"
import { runCols } from "@/lib/run-cols"

const BRANCH = "rounded-full bg-branch px-2 py-0.5 font-mono text-xs text-primary"
const GHR = "rounded-full bg-muted px-2 py-0.5 font-mono text-xs text-foreground"

function ActionRow({ run, now, wide }: { run: ActionsRun; now: number; wide: boolean }) {
  const by = [run.event, run.actor && `by ${run.actor}`].filter(Boolean).join(" ")
  return (
    <RunRow
      wide={wide}
      icon={<RunStatusIcon status={run.status} conclusion={run.conclusion} />}
      title={
        <Link to="/actions/$repo/runs/$id" params={{ repo: run.repo, id: String(run.id) }} className="hover:text-primary hover:underline">
          {run.title}
          <span className="sr-only">{`, ${run.repo} #${run.run_number}`}</span>
        </Link>
      }
      facts={
        <>
          <RepoFact name={run.repo} />
          <span>{`${run.workflow} #${run.run_number}`}</span>
          {by && <span>{by}</span>}
          {run.branch && <span className={BRANCH}>{run.branch}</span>}
          {run.ghr && <span className={GHR}>ghr</span>}
        </>
      }
      values={
        <>
          <RunValue kind="time">{startText(run.started_at, now)}</RunValue>
          <RunValue kind="duration">{dur(runMs(run, now))}</RunValue>
        </>
      }
    />
  )
}

// The In progress group shows when it has runs, or always when showActive
// is set, so an empty one can say that nothing runs.
export function ActionsRunTable({
  active,
  recent,
  now,
  wide,
  showActive,
}: {
  active: ActionsRun[]
  recent: ActionsRun[]
  now: number
  wide: boolean
  showActive: boolean
}) {
  const cols = runCols(wide, false)
  return (
    <div className="overflow-x-auto">
      <table className="w-full border-collapse">
        {(active.length > 0 || showActive) && (
          <tbody>
            <RunGroup label="In progress" cols={cols} />
            {active.length === 0 ? (
              <tr>
                <td colSpan={cols} className="px-4 py-3 text-sm text-muted-foreground">
                  Nothing is running or queued.
                </td>
              </tr>
            ) : (
              active.map((r) => <ActionRow key={`${r.repo}#${r.id}`} run={r} now={now} wide={wide} />)
            )}
          </tbody>
        )}
        {groupByDayOf(recent, (r) => r.started_at, now).map((g) => (
          <tbody key={g.key}>
            <RunGroup label={g.label} cols={cols} />
            {g.rows.map((r) => (
              <ActionRow key={`${r.repo}#${r.id}`} run={r} now={now} wide={wide} />
            ))}
          </tbody>
        ))}
      </table>
    </div>
  )
}
