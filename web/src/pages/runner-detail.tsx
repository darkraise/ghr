import type { UseQueryResult } from "@tanstack/react-query"
import { useNavigate, useParams, useSearch } from "@tanstack/react-router"
import { Alert, AlertDescription, AlertTitle } from "darkraise-ui/components/alert"
import { Button } from "darkraise-ui/components/button"
import { Label } from "darkraise-ui/components/label"
import { Spinner } from "darkraise-ui/components/spinner"
import { Switch } from "darkraise-ui/components/switch"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "darkraise-ui/components/table"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "darkraise-ui/components/tabs"
import { PageHeader } from "darkraise-ui/layout"
import { useEffect, useState } from "react"
import { useContainers, useLogTail, useStatus, useSteps } from "@/api/hooks"
import type { Container, InstanceStatus, Step } from "@/api/types"
import { LogView } from "@/components/log-view"
import { Glyph } from "@/components/glyph"
import { StopRunnerDialog } from "@/components/runners-table"
import { StateBadge } from "@/components/state-badge"
import { dateTimeSec, dur, isZeroTime } from "@/lib/format"
import { useNow } from "@/lib/use-now"
import { errorText } from "@/query"

export type DetailTab = "steps" | "log" | "containers"

function stepIcon(s: Step) {
  if (s.status === "in_progress") {
    return (
      <span className="inline-flex items-center">
        <Spinner size="sm" />
        <span className="sr-only">running</span>
      </span>
    )
  }
  if (s.conclusion === "success") return <Glyph symbol="✔" label="succeeded" className="text-green-600" />
  if (s.conclusion === "failure") return <Glyph symbol="✖" label="failed" className="text-destructive" />
  if (s.conclusion === "cancelled") return <Glyph symbol="⊘" label="cancelled" className="text-muted-foreground" />
  if (s.conclusion === "skipped") return <Glyph symbol="–" label="skipped" className="text-muted-foreground" />
  return <Glyph symbol="○" label="pending" className="text-muted-foreground" />
}

function StepList({ steps }: { steps: UseQueryResult<Step[]> }) {
  return (
    <>
      {steps.isError && <p className="mb-2 text-sm text-destructive">{errorText(steps.error)}</p>}
      {steps.data && steps.data.length > 0 ? (
        <ul className="flex flex-col gap-1">
          {steps.data.map((s) => (
            <li key={s.number} className="flex items-center gap-2">
              {stepIcon(s)}
              <span>{s.name}</span>
            </li>
          ))}
        </ul>
      ) : (
        <p className="text-sm text-muted-foreground">no steps reported yet</p>
      )}
    </>
  )
}

function ContainerTable({ containers }: { containers: UseQueryResult<Container[]> }) {
  return (
    <>
      {containers.isError && <p className="mb-2 text-sm text-destructive">{errorText(containers.error)}</p>}
      {containers.data && containers.data.length > 0 ? (
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
                <TableCell>{c.state}</TableCell>
                <TableCell>{c.name}</TableCell>
                <TableCell>{c.image}</TableCell>
                <TableCell className="text-muted-foreground">{c.project}</TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      ) : (
        <p className="text-sm text-muted-foreground">none in this runner's compose projects</p>
      )}
    </>
  )
}

export function RunnerDetailPage() {
  const { id = "" } = useParams({ strict: false }) as { id?: string }
  const { tab = "steps" } = useSearch({ strict: false }) as { tab?: DetailTab }
  const navigate = useNavigate()
  const status = useStatus()
  const now = useNow()
  const [snapshot, setSnapshot] = useState<InstanceStatus | undefined>(undefined)
  const [finishedAt, setFinishedAt] = useState<number | undefined>(undefined)
  const [follow, setFollow] = useState(true)
  const [stopping, setStopping] = useState(false)

  const current = status.data?.instances.find((i) => i.id === id)
  const live = current !== undefined
  const finished = status.data !== undefined && !live

  // The detail page outlives the runner: it keeps the last instance it saw,
  // and stops polling steps and containers once the runner leaves /status.
  useEffect(() => {
    if (current) {
      setSnapshot(current)
      setFinishedAt(undefined)
    } else if (status.data && finishedAt === undefined) {
      setFinishedAt(Date.now())
    }
  }, [current, status.data, finishedAt])

  const steps = useSteps(id, live)
  const containers = useContainers(id, live)
  const log = useLogTail(id, tab === "log")

  const inst = current ?? snapshot
  const job = inst?.job
  const start = job && !isZeroTime(job.started_at) ? job.started_at : inst?.since
  const end = finished ? (finishedAt ?? now) : now

  return (
    <>
      <PageHeader
        breadcrumbs={[{ label: "Runners", href: "/runners" }, { label: id }]}
        title={id}
        actions={
          <div className="flex gap-2">
            {job?.html_url && (
              <Button variant="outline" asChild>
                <a href={job.html_url} target="_blank" rel="noreferrer">
                  Open run
                </a>
              </Button>
            )}
            <Button variant="destructive" disabled={!live || status.isError} onClick={() => setStopping(true)}>
              Stop runner
            </Button>
          </div>
        }
      />
      {finished && (
        <Alert variant="warning" className="mb-4">
          <AlertTitle>This runner has finished</AlertTitle>
          <AlertDescription>
            <Button variant="link" className="px-0" onClick={() => void navigate({ to: "/runners" })}>
              Back to runners
            </Button>
          </AlertDescription>
        </Alert>
      )}
      {inst && (
        <p className="mb-4 flex flex-wrap items-center gap-2 text-sm">
          <StateBadge state={inst.state} />
          <span>· {inst.repo}</span>
          {job && <span>· {`${job.name} #${job.run_number}`}</span>}
          {start && <span>· {dur(end - Date.parse(start))}</span>}
          {start && <span>· started {dateTimeSec(start)}</span>}
        </p>
      )}
      <Tabs value={tab} onValueChange={(t) => void navigate({ to: "/runners/$id", params: { id }, search: { tab: t as DetailTab } })}>
        <TabsList>
          <TabsTrigger value="steps">Steps</TabsTrigger>
          <TabsTrigger value="log">Log</TabsTrigger>
          <TabsTrigger value="containers">Containers</TabsTrigger>
        </TabsList>
        <TabsContent value="steps">
          <StepList steps={steps} />
        </TabsContent>
        <TabsContent value="log">
          <div className="mb-2 flex items-center gap-2">
            <Switch id="follow" checked={follow} onCheckedChange={setFollow} />
            <Label htmlFor="follow">Follow</Label>
          </div>
          {log.isError && <p className="mb-2 text-sm text-destructive">{errorText(log.error)}</p>}
          <LogView text={log.data?.text ?? ""} follow={follow} onFollowChange={setFollow} />
        </TabsContent>
        <TabsContent value="containers">
          <ContainerTable containers={containers} />
        </TabsContent>
      </Tabs>
      <StopRunnerDialog instance={stopping && inst ? inst : null} onClose={() => setStopping(false)} />
    </>
  )
}
