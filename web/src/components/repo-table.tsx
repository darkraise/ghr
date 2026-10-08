import { Button } from "darkraise-ui/components/button"
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "darkraise-ui/components/dropdown-menu"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "darkraise-ui/components/table"
import { Tooltip, TooltipContent, TooltipTrigger } from "darkraise-ui/components/tooltip"
import { Ellipsis } from "lucide-react"
import type { ActivityRepo, RepoStatus } from "@/api/types"
import { ConfirmDialog } from "@/components/confirm-dialog"
import { RepoActivityStrip } from "@/components/repo-activity-strip"
import { ResultIcon } from "@/components/result-icon"
import { ago } from "@/lib/format"
import { repoStateWord } from "@/lib/status"
import { useRepoActions } from "@/lib/use-repo-actions"

const STATE_CLASS: Record<string, string | undefined> = {
  Running: "text-primary",
  Paused: "text-muted-foreground",
  Removing: "text-muted-foreground",
  Error: "text-destructive",
}

function RunnerCells({ repo }: { repo: RepoStatus }) {
  if (repo.max === 0) return <span className="font-mono">{repo.active}</span>
  return (
    <span className="flex items-center gap-2">
      <span className="font-mono">
        {repo.active}/{repo.max}
      </span>
      <span aria-hidden="true" className="flex gap-0.5">
        {Array.from({ length: repo.max }, (_, i) => (
          <span
            key={i}
            data-slot="runner"
            data-filled={i < repo.active ? "true" : undefined}
            className={`h-2.5 w-1.5 rounded-[1px] ${i < repo.active ? "bg-primary" : "bg-muted"}`}
          />
        ))}
      </span>
    </span>
  )
}

// Changing a cap is rare, so the max stepper lives on the repository page and
// the row keeps one menu.
function RowMenu({ repo, offline, onOpen }: { repo: RepoStatus; offline: boolean; onOpen: (name: string) => void }) {
  const actions = useRepoActions(repo, offline)
  return (
    <>
      <DropdownMenu>
        <Tooltip>
          <TooltipTrigger asChild>
            <DropdownMenuTrigger asChild>
              <Button size="icon" variant="ghost" aria-label={`More actions for ${repo.name}`}>
                <Ellipsis size={15} aria-hidden="true" />
              </Button>
            </DropdownMenuTrigger>
          </TooltipTrigger>
          <TooltipContent>More actions</TooltipContent>
        </Tooltip>
        <DropdownMenuContent align="end">
          <DropdownMenuItem disabled={actions.locked} onSelect={actions.togglePause}>
            {repo.paused ? "Resume" : "Pause"}
          </DropdownMenuItem>
          <DropdownMenuItem onSelect={() => onOpen(repo.name)}>Edit</DropdownMenuItem>
          <DropdownMenuItem disabled={actions.locked} onSelect={actions.askRemove} className="text-destructive">
            Remove
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
      <ConfirmDialog confirm={actions.confirm} onClose={actions.closeConfirm} />
    </>
  )
}

export function RepoTable({
  repos,
  activity,
  now,
  offline,
  onOpen,
}: {
  repos: RepoStatus[]
  activity: ActivityRepo[] | undefined
  now: number
  offline: boolean
  onOpen: (name: string) => void
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
          const hours = activity?.find((a) => a.repo === r.name)?.hours
          return (
            <TableRow key={r.name} className="cursor-pointer" onClick={() => onOpen(r.name)}>
              <TableCell>
                <a
                  href={`/repositories/${encodeURIComponent(r.name)}`}
                  className="font-medium hover:underline"
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
              <TableCell className={STATE_CLASS[word] ?? ""}>{word}</TableCell>
              <TableCell>
                <RunnerCells repo={r} />
              </TableCell>
              <TableCell>{r.queued > 0 && <span className="font-mono text-warning">{r.queued}</span>}</TableCell>
              <TableCell>{hours && <RepoActivityStrip repo={r.name} hours={hours} />}</TableCell>
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
                <RowMenu repo={r} offline={offline} onOpen={onOpen} />
              </TableCell>
            </TableRow>
          )
        })}
      </TableBody>
    </Table>
  )
}
