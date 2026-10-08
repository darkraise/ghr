import type { UseQueryResult } from "@tanstack/react-query"
import { Link, useNavigate } from "@tanstack/react-router"
import { Button } from "darkraise-ui/components/button"
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "darkraise-ui/components/dropdown-menu"
import { Label } from "darkraise-ui/components/label"
import { Spinner } from "darkraise-ui/components/spinner"
import { Switch } from "darkraise-ui/components/switch"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "darkraise-ui/components/table"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "darkraise-ui/components/tabs"
import { Tooltip, TooltipContent, TooltipTrigger } from "darkraise-ui/components/tooltip"
import { ChevronLeft, Ellipsis, ExternalLink } from "lucide-react"
import { useEffect, useState } from "react"
import { useContainers, useLogTail, useStatus, useSteps } from "@/api/hooks"
import type { Container, InstanceStatus, Step } from "@/api/types"
import { LogView } from "@/components/log-view"
import { ErrorLine } from "@/components/page/error-line"
import { StateText } from "@/components/page/state-text"
import { StepList } from "@/components/step-list"
import { StopRunnerDialog } from "@/components/stop-runner-dialog"
import { copyWithToast } from "@/lib/clipboard"
import { dateTimeSec, dur, startedAt } from "@/lib/format"
import { useNow } from "@/lib/use-now"
import { errorText } from "@/query"

export type DetailTab = "steps" | "log" | "containers"

function Steps({ steps, live, finished, now }: { steps: UseQueryResult<Step[]>; live: boolean; finished: boolean; now: number }) {
  if (steps.isError) return <ErrorLine>{errorText(steps.error)}</ErrorLine>
  if (!steps.data) {
    // Steps are polled only while the runner is live.
    if (finished) return <p className="text-sm text-muted-foreground">Steps are not kept after a runner finishes.</p>
    return <Spinner label="Loading" />
  }
  if (steps.data.length === 0) return <p className="text-sm text-muted-foreground">No steps reported yet.</p>
  return <StepList steps={steps.data} now={now} live={live} />
}

function Containers({ containers }: { containers: UseQueryResult<Container[]> }) {
  if (containers.isError) return <ErrorLine>{errorText(containers.error)}</ErrorLine>
  if (!containers.data || containers.data.length === 0) {
    return <p className="text-sm text-muted-foreground">No containers in this runner's compose projects.</p>
  }
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>State</TableHead>
          <TableHead>Name</TableHead>
          <TableHead>Image</TableHead>
          <TableHead>Project</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {containers.data.map((c) => (
          <TableRow key={c.id}>
            <TableCell>
              <StateText state={c.state} />
            </TableCell>
            <TableCell className="font-mono">{c.name}</TableCell>
            <TableCell className="font-mono">{c.image}</TableCell>
            <TableCell className="text-muted-foreground">{c.project}</TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  )
}

function MoreMenu({ id, runUrl }: { id: string; runUrl: string | undefined }) {
  return (
    <DropdownMenu>
      <Tooltip>
        <TooltipTrigger asChild>
          <DropdownMenuTrigger asChild>
            <Button size="icon" variant="ghost" aria-label={`More actions for runner ${id}`}>
              <Ellipsis size={15} aria-hidden="true" />
            </Button>
          </DropdownMenuTrigger>
        </TooltipTrigger>
        <TooltipContent>More actions</TooltipContent>
      </Tooltip>
      <DropdownMenuContent align="end">
        {runUrl && <DropdownMenuItem onSelect={() => void copyWithToast(runUrl, "run URL")}>Copy run URL</DropdownMenuItem>}
        <DropdownMenuItem onSelect={() => void copyWithToast(id, id)}>Copy runner ID</DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

// The panel outlives the runner: it keeps the last instance it saw and stops
// polling steps and containers once the runner leaves /status. Callers key it
// by ID, so another runner starts clean.
export function RunnerPanel({ id, tab, backLink }: { id: string; tab: DetailTab; backLink: boolean }) {
  const navigate = useNavigate()
  const status = useStatus()
  const now = useNow()
  const [kept, setKept] = useState<InstanceStatus | undefined>(undefined)
  const [endedAt, setEndedAt] = useState<number | undefined>(undefined)
  const [follow, setFollow] = useState(true)
  const [stopping, setStopping] = useState(false)

  const current = status.data?.instances.find((i) => i.id === id)
  const live = current !== undefined
  const finished = status.data !== undefined && !live

  useEffect(() => {
    if (current) {
      if (kept !== current) setKept(current)
      setEndedAt(undefined)
    } else if (status.data && endedAt === undefined) {
      setEndedAt(now)
    }
  }, [current, kept, status.data, endedAt, now])

  const steps = useSteps(id, live)
  const containers = useContainers(id, live)
  const log = useLogTail(id, tab === "log")

  const inst = current ?? kept
  const job = inst?.job
  const runUrl = job?.html_url
  const start = inst ? startedAt(inst) : undefined
  const end = finished ? (endedAt ?? now) : now

  return (
    <div className="flex min-w-0 flex-col gap-4">
      <div className="flex flex-col gap-3 rounded-[10px] border border-border bg-card p-4">
        {backLink && (
          <Link to="/runners" aria-label="Back to Runners" className="inline-flex items-center gap-1 self-start text-sm text-primary hover:underline">
            <ChevronLeft size={15} aria-hidden="true" />
            Runners
          </Link>
        )}
        <div className="flex flex-wrap items-center gap-x-3 gap-y-2">
          <h2 className="font-mono text-lg font-semibold">{id}</h2>
          {finished ? <StateText state="finished" /> : inst && <StateText state={inst.state} />}
          <div className="ml-auto flex flex-wrap items-center gap-2">
            {runUrl && (
              <Button variant="outline" asChild>
                <a href={runUrl} target="_blank" rel="noreferrer">
                  <ExternalLink size={15} aria-hidden="true" />
                  Open run
                </a>
              </Button>
            )}
            <Button variant="destructive" disabled={!live || status.isError} onClick={() => setStopping(true)}>
              Stop runner
            </Button>
            <MoreMenu id={id} runUrl={runUrl} />
          </div>
        </div>
        {finished && <p className="text-sm text-muted-foreground">This runner has finished</p>}
        {inst && (
          <dl className="grid grid-cols-[max-content_minmax(0,1fr)] gap-x-4 gap-y-1 text-sm sm:grid-cols-[max-content_minmax(0,1fr)_max-content_minmax(0,1fr)]">
            <dt className="text-muted-foreground">Repository</dt>
            <dd>{inst.repo}</dd>
            {job && (
              <>
                <dt className="text-muted-foreground">Job</dt>
                <dd>
                  {job.name} <span className="font-mono">#{job.run_number}</span>
                </dd>
              </>
            )}
            {start && (
              <>
                <dt className="text-muted-foreground">{finished ? "Ran for" : "Running for"}</dt>
                <dd className="font-mono">{dur(end - Date.parse(start))}</dd>
                <dt className="text-muted-foreground">Started</dt>
                <dd className="font-mono">{dateTimeSec(start)}</dd>
              </>
            )}
          </dl>
        )}
      </div>
      <Tabs value={tab} onValueChange={(t) => void navigate({ to: "/runners/$id", params: { id }, search: { tab: t as DetailTab } })}>
        <TabsList>
          <TabsTrigger value="steps">Steps</TabsTrigger>
          <TabsTrigger value="log">Log</TabsTrigger>
          <TabsTrigger value="containers">Containers</TabsTrigger>
        </TabsList>
        <TabsContent value="steps">
          <Steps steps={steps} live={live} finished={finished} now={now} />
        </TabsContent>
        <TabsContent value="log">
          <div className="mb-2 flex items-center gap-2">
            <Switch id="follow" checked={follow} onCheckedChange={setFollow} />
            <Label htmlFor="follow">Follow</Label>
          </div>
          {log.isError && <ErrorLine>{errorText(log.error)}</ErrorLine>}
          <LogView text={log.data?.text ?? ""} follow={follow} onFollowChange={setFollow} className="h-[calc(100vh-24rem)] min-h-64" />
        </TabsContent>
        <TabsContent value="containers">
          <Containers containers={containers} />
        </TabsContent>
      </Tabs>
      <StopRunnerDialog instance={stopping && inst ? inst : null} onClose={() => setStopping(false)} />
    </div>
  )
}
