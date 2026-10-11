import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "darkraise-ui/components/table"
import type { ActivityRepo, RepoStatus } from "@/api/types"
import { StatusPill } from "@/components/page/status-pill"
import { RepoActivityStrip } from "@/components/repo-activity-strip"
import { RepoRowMenu } from "@/components/repo-row-menu"
import { ResultIcon } from "@/components/result-icon"
import { RunnerMeter } from "@/components/runner-meter"
import { ago } from "@/lib/format"
import { repoStateWord } from "@/lib/status"

function RunnerCells({ repo }: { repo: RepoStatus }) {
  if (repo.max === 0) return <span className="font-mono">{repo.active}</span>
  return (
    <span className="flex items-center gap-2">
      <span className="font-mono">
        {repo.active}/{repo.max}
      </span>
      <RunnerMeter active={repo.active} max={repo.max} />
    </span>
  )
}

export function RepoTable({
  repos,
  activity,
  now,
  offline,
  onOpen,
  onEdit,
}: {
  repos: RepoStatus[]
  activity: ActivityRepo[] | undefined
  now: number
  offline: boolean
  onOpen: (name: string) => void
  onEdit: (name: string) => void
}) {
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>Repository</TableHead>
          <TableHead>State</TableHead>
          <TableHead>Runners</TableHead>
          <TableHead>Waiting</TableHead>
          <TableHead>Last 24 hours</TableHead>
          <TableHead>Last job</TableHead>
          <TableHead>
            <span className="sr-only">Actions</span>
          </TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {repos.map((r) => {
          const word = repoStateWord(r)
          const seen = activity?.find((a) => a.repo.toLowerCase() === r.name.toLowerCase())
          return (
            <TableRow key={r.name} className="cursor-pointer" onClick={() => onOpen(r.name)}>
              <TableCell>
                <a
                  href={`/repositories/${encodeURIComponent(r.name)}`}
                  className="font-semibold hover:underline"
                  onClick={(e) => {
                    e.preventDefault()
                    e.stopPropagation()
                    onOpen(r.name)
                  }}
                >
                  {r.name}
                </a>
                {r.error && <p className="text-xs text-destructive">{r.error}</p>}
              </TableCell>
              <TableCell>
                <StatusPill state={word.toLowerCase()} />
              </TableCell>
              <TableCell>
                <RunnerCells repo={r} />
              </TableCell>
              <TableCell>{r.queued > 0 && <span className="font-mono text-warning">{r.queued}</span>}</TableCell>
              <TableCell>{seen && <RepoActivityStrip repo={r.name} hours={seen.hours} />}</TableCell>
              <TableCell>
                {r.last_job && (
                  <span className="flex items-center gap-1.5 whitespace-nowrap">
                    <ResultIcon conclusion={r.last_job.conclusion} />
                    <span>
                      {r.last_job.job_name} <span className="font-mono">#{r.last_job.run_number}</span>
                    </span>
                    <span className="text-muted-foreground">{ago(now - Date.parse(r.last_job.finished_at))}</span>
                  </span>
                )}
              </TableCell>
              <TableCell className="text-right" onClick={(e) => e.stopPropagation()}>
                <RepoRowMenu repo={r} offline={offline} onEdit={onEdit} />
              </TableCell>
            </TableRow>
          )
        })}
      </TableBody>
    </Table>
  )
}
