import { useMutation, useQueryClient, type UseQueryResult } from "@tanstack/react-query"
import { Link } from "@tanstack/react-router"
import { Button } from "darkraise-ui/components/button"
import { Card, CardContent, CardHeader, CardTitle } from "darkraise-ui/components/card"
import { toast } from "darkraise-ui/components/sonner"
import { Spinner } from "darkraise-ui/components/spinner"
import { Stat, StatLabel, StatValue } from "darkraise-ui/components/stat"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "darkraise-ui/components/table"
import { PageHeader } from "darkraise-ui/layout"
import { useEffect, useRef, useState, type ReactNode } from "react"
import { api } from "@/api/client"
import { keys, useEvents, useMetrics, useStatus } from "@/api/hooks"
import type { ConfigPatch, GhrEvent, HistoryEntry, Metrics, RepoStatus, Status } from "@/api/types"
import { AddRepoDialog } from "@/components/add-repo-dialog"
import { Glyph } from "@/components/glyph"
import { RepoActionButtons } from "@/components/repo-actions"
import { RunnersTable } from "@/components/runners-table"
import { Sparkline } from "@/components/sparkline"
import { StateBadge } from "@/components/state-badge"
import { Stepper } from "@/components/stepper"
import { ago, clock, fmtMem, maxText, series } from "@/lib/format"
import { capText, repoState, running } from "@/lib/status"
import { useNow } from "@/lib/use-now"
import { errorText } from "@/query"

function allPaused(repos: RepoStatus[]): boolean {
  const live = repos.filter((r) => !r.removing)
  return live.length > 0 && live.every((r) => r.paused)
}

const jobGlyph: Record<string, { symbol: string; label: string; colour: string }> = {
  success: { symbol: "✔", label: "succeeded", colour: "text-green-600" },
  failure: { symbol: "✖", label: "failed", colour: "text-destructive" },
  cancelled: { symbol: "⊘", label: "cancelled", colour: "text-muted-foreground" },
  skipped: { symbol: "–", label: "skipped", colour: "text-muted-foreground" },
}

function LastJob({ job, now }: { job: HistoryEntry; now: number }) {
  const g = jobGlyph[job.conclusion] ?? jobGlyph.failure
  return (
    <>
      <Glyph symbol={g?.symbol ?? "✖"} label={g?.label ?? "failed"} className={g?.colour} />
      {` #${job.run_number} ${job.job_name}  ${ago(now - Date.parse(job.finished_at))}`}
    </>
  )
}

function Tile({ label, value, children }: { label: string; value: string; children?: ReactNode }) {
  return (
    <Card>
      <CardContent className="p-4">
        <Stat>
          <StatLabel>{label}</StatLabel>
          <StatValue>{value}</StatValue>
        </Stat>
        {children}
      </CardContent>
    </Card>
  )
}

function StatTiles({ status, metrics }: { status: Status; metrics: UseQueryResult<Metrics> }) {
  const m = metrics.data
  const samples = m?.samples ?? []
  const failed = metrics.isError ? `✖ ${errorText(metrics.error)}` : undefined
  const queued = status.repos.reduce((n, r) => n + r.queued, 0)
  const cpu = m?.cpu !== undefined ? `${m.cpu.toFixed(0)}%` : "–"
  const mem = m?.mem_used !== undefined && m.mem_total !== undefined ? `${fmtMem(m.mem_used)} / ${fmtMem(m.mem_total)}` : "–"
  return (
    <div className="mb-4 grid gap-4 sm:grid-cols-2 xl:grid-cols-5">
      <Tile label="Running" value={`${running(status)} / ${capText(status)}`}>
        <Sparkline label="running" values={series(samples, (s) => s.live)} max={status.mode === "all" ? undefined : status.global_max} />
      </Tile>
      <Tile label="Queued jobs" value={String(queued)}>
        <Sparkline label="queued jobs" values={series(samples, (s) => s.queued)} />
      </Tile>
      <Tile label="CPU" value={failed ?? cpu}>
        <Sparkline label="cpu" values={series(samples, (s) => s.cpu)} max={100} />
      </Tile>
      <Tile label="Memory" value={failed ?? mem} />
      <Tile label="Disk" value={`${status.disk_pct}%`} />
    </div>
  )
}

function RepoTable({
  repos,
  now,
  offline,
  busy,
  onMax,
}: {
  repos: RepoStatus[]
  now: number
  offline: boolean
  busy: boolean
  onMax: (name: string, max: number) => void
}) {
  if (repos.length === 0) {
    return <p className="py-6 text-center text-sm text-muted-foreground">no repos configured</p>
  }
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>Repo</TableHead>
          <TableHead>State</TableHead>
          <TableHead>Run</TableHead>
          <TableHead>Queue</TableHead>
          <TableHead>Last job</TableHead>
          <TableHead className="text-right">Actions</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {repos.map((r) => (
          <TableRow key={r.name}>
            <TableCell>{r.name}</TableCell>
            <TableCell>
              <StateBadge state={repoState(r)} />
            </TableCell>
            <TableCell>
              {r.active}/{maxText(r.max)}
            </TableCell>
            <TableCell>
              {r.queued > 0 ? (
                <span className="text-amber-600">
                  <Glyph symbol="⧗" label="queued" /> {r.queued}
                </span>
              ) : (
                "–"
              )}
            </TableCell>
            <TableCell>
              {r.last_job ? <LastJob job={r.last_job} now={now} /> : "–"}
              {r.error && <span className="ml-2 text-destructive">{r.error}</span>}
            </TableCell>
            <TableCell>
              <div className="flex flex-wrap justify-end gap-2">
                <Stepper
                  label={`max for ${r.name}`}
                  value={r.max}
                  text={maxText(r.max)}
                  disabled={offline || busy || r.max === 0}
                  title={r.max === 0 ? "unlimited: change it on the repository page" : undefined}
                  onChange={(n) => onMax(r.name, n)}
                />
                <Button size="sm" variant="outline" asChild>
                  <Link to="/repositories/$name" params={{ name: r.name }} aria-label={`Edit ${r.name}`}>
                    Edit
                  </Link>
                </Button>
                <RepoActionButtons repo={r} offline={offline} />
              </div>
            </TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  )
}

const eventIcon: Record<string, string> = { ok: "✔", warn: "⚠", error: "✖" }
const eventLabel: Record<string, string> = { ok: "ok", warn: "warning", error: "error" }
const eventColour: Record<string, string> = { ok: "text-green-600", warn: "text-amber-600", error: "text-destructive" }

function ActivityFeed({ events }: { events: GhrEvent[] }) {
  const ref = useRef<HTMLDivElement>(null)
  useEffect(() => {
    const el = ref.current
    if (el) el.scrollTop = el.scrollHeight
  }, [events])
  if (events.length === 0) return <p className="py-6 text-center text-sm text-muted-foreground">no activity yet</p>
  return (
    <div ref={ref} className="max-h-80 overflow-auto font-mono text-xs">
      <ul className="flex flex-col gap-1">
        {events.map((e) => (
          <li key={e.seq} className="flex gap-2">
            <span className="text-muted-foreground">{clock(e.time)}</span>
            <Glyph symbol={eventIcon[e.level] ?? "▶"} label={eventLabel[e.level] ?? "info"} className={eventColour[e.level] ?? "text-primary"} />
            <span className="w-24 shrink-0 truncate">{e.repo ?? ""}</span>
            <span>{e.msg}</span>
          </li>
        ))}
      </ul>
    </div>
  )
}

export function DashboardPage() {
  const status = useStatus()
  const metrics = useMetrics()
  const events = useEvents(status.data?.epoch)
  const now = useNow()
  const queryClient = useQueryClient()
  const [adding, setAdding] = useState(false)
  const toggle = useMutation({
    mutationFn: (resume: boolean) => (resume ? api.resumeAll() : api.pauseAll()),
    onSuccess: (_data, resume) => {
      toast.success(resume ? "resumed all repos" : "paused all repos (drain)")
    },
    onSettled: () => queryClient.invalidateQueries({ queryKey: keys.status }),
  })
  const patch = useMutation({
    mutationFn: ({ body }: { body: ConfigPatch; done: string }) => api.patchConfig(body),
    onSuccess: (_data, { done }) => {
      toast.success(done)
    },
    onSettled: () =>
      Promise.all([
        queryClient.invalidateQueries({ queryKey: keys.status }),
        queryClient.invalidateQueries({ queryKey: keys.config }),
      ]),
  })

  const st = status.data
  if (!st) {
    return (
      <>
        <PageHeader title="Dashboard" />
        <Spinner label="waiting for the daemon…" />
      </>
    )
  }
  const offline = status.isError
  const paused = allPaused(st.repos)
  const otherMode = st.mode === "all" ? "queue" : "all"
  return (
    <>
      <PageHeader
        title="Dashboard"
        actions={
          <div className="flex flex-wrap items-center gap-2">
            <Button
              variant="outline"
              disabled={offline || patch.isPending}
              onClick={() => patch.mutate({ body: { mode: otherMode }, done: `mode ${otherMode}` })}
            >
              Switch to {otherMode.toUpperCase()}
            </Button>
            <span className="text-sm text-muted-foreground">global max</span>
            <Stepper
              label="global max"
              value={st.global_max}
              disabled={offline || patch.isPending}
              title="applies in queue mode"
              onChange={(n) => patch.mutate({ body: { global_max: n }, done: `global max ${n}` })}
            />
            <Button variant="secondary" disabled={offline || toggle.isPending} onClick={() => toggle.mutate(paused)}>
              {paused ? "Resume all" : "Pause all"}
            </Button>
          </div>
        }
      />
      <StatTiles status={st} metrics={metrics} />
      <div className="mb-4 grid gap-4 lg:grid-cols-2">
        <Card>
          <CardHeader className="flex flex-row items-center justify-between">
            <CardTitle>Repositories</CardTitle>
            <Button size="sm" disabled={offline} onClick={() => setAdding(true)}>
              + Add repository
            </Button>
          </CardHeader>
          <CardContent>
            <RepoTable
              repos={st.repos}
              now={now}
              offline={offline}
              busy={patch.isPending}
              onMax={(name, max) => patch.mutate({ body: { repos: { [name]: { max } } }, done: `${name} max ${max}` })}
            />
          </CardContent>
        </Card>
        <Card>
          <CardHeader>
            <CardTitle>Runners</CardTitle>
          </CardHeader>
          <CardContent>
            <RunnersTable status={st} actions />
          </CardContent>
        </Card>
      </div>
      <Card>
        <CardHeader>
          <CardTitle>Activity</CardTitle>
        </CardHeader>
        <CardContent>
          <ActivityFeed events={events} />
        </CardContent>
      </Card>
      <AddRepoDialog open={adding} onClose={() => setAdding(false)} />
    </>
  )
}
