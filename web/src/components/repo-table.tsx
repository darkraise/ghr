import { Button } from "darkraise-ui/components/button"
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "darkraise-ui/components/dropdown-menu"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "darkraise-ui/components/table"
import { Tooltip, TooltipContent, TooltipTrigger } from "darkraise-ui/components/tooltip"
import { Ellipsis } from "lucide-react"
import type { ActivityRepo, Config, RepoStatus } from "@/api/types"
import { ConfirmDialog } from "@/components/confirm-dialog"
import { RepoActivityStrip } from "@/components/repo-activity-strip"
import { ResultIcon } from "@/components/result-icon"
import { RunnerMeter } from "@/components/runner-meter"
import { ago } from "@/lib/format"
import { weekRate, weekText } from "@/lib/repos"
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
      <RunnerMeter active={repo.active} max={repo.max} />
    </span>
  )
}

const EXTRA = "hidden md:table-cell"

// Changing a cap is rare, so the max field lives on the repository page and
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
  columns = "compact",
  config,
}: {
  repos: RepoStatus[]
  activity: ActivityRepo[] | undefined
  now: number
  offline: boolean
  onOpen: (name: string) => void
  columns?: "compact" | "full"
  config?: Config
}) {
  const full = columns === "full"
  const warmShown = full && config?.mode === "all"
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>Repository</TableHead>
          <TableHead>State</TableHead>
          <TableHead>Runners</TableHead>
          <TableHead>Waiting</TableHead>
          {warmShown && <TableHead className={`${EXTRA} text-right`}>Warm</TableHead>}
          <TableHead>Last 24 hours</TableHead>
          {full && <TableHead className={`${EXTRA} text-right`}>7 days</TableHead>}
          <TableHead>Last job</TableHead>
          {full && <TableHead className={EXTRA}>Labels</TableHead>}
          <TableHead>
            <span className="sr-only">Actions</span>
          </TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {repos.map((r) => {
          const word = repoStateWord(r)
          const seen = activity?.find((a) => a.repo.toLowerCase() === r.name.toLowerCase())
          const cfg = config?.repos?.find((c) => c.name === r.name)
          const rate = weekRate(seen?.week)
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
                {full && r.removing && <p className="text-xs text-muted-foreground">Removing. Running jobs finish first.</p>}
              </TableCell>
              <TableCell className={STATE_CLASS[word] ?? ""}>{word}</TableCell>
              <TableCell>
                <RunnerCells repo={r} />
              </TableCell>
              <TableCell>{r.queued > 0 && <span className="font-mono text-warning">{r.queued}</span>}</TableCell>
              {warmShown && (
                <TableCell data-testid="warm" className={`${EXTRA} text-right font-mono`}>
                  {cfg ? (cfg.warm ?? 1) : ""}
                </TableCell>
              )}
              <TableCell>{seen && <RepoActivityStrip repo={r.name} hours={seen.hours} />}</TableCell>
              {full && (
                <TableCell className={`${EXTRA} text-right`}>
                  {rate !== undefined && seen && (
                    <>
                      <span className="font-mono">{`${rate}%`}</span>
                      <span className="sr-only">{` ${weekText(seen.week)}`}</span>
                    </>
                  )}
                </TableCell>
              )}
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
              {full && (
                <TableCell className={`${EXTRA} whitespace-normal`}>
                  <span className="font-mono text-xs break-words text-muted-foreground">{(cfg?.labels ?? []).join(" ")}</span>
                </TableCell>
              )}
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
