import { useMutation, useQueryClient } from "@tanstack/react-query"
import { Link } from "@tanstack/react-router"
import { Button } from "darkraise-ui/components/button"
import { toast } from "darkraise-ui/components/sonner"
import { Spinner } from "darkraise-ui/components/spinner"
import { useState } from "react"
import { api } from "@/api/client"
import { keys, useStatus } from "@/api/hooks"
import type { GhrUpdate, JobStatus, Status } from "@/api/types"
import { ConfirmDialog, type Confirm } from "@/components/confirm-dialog"
import { Field, FieldGroup } from "@/components/page/field"
import { StatusPill } from "@/components/page/status-pill"
import { RestartControl } from "@/components/restart-banner"
import { RunnerSection } from "@/components/runner-section"
import { ago } from "@/lib/format"
import { useNow } from "@/lib/use-now"
import { errorText } from "@/query"

const CHECKS = new Set(["ghr_update_check", "runner_update_check"])

const STEPS: Record<string, string> = {
  download: "Downloading",
  verify: "Verifying the checksum and the archive",
  smoke: "Testing the new binary",
  swap: "Installing the files",
}

// The daemon cancels an install only until its swap begins.
const CANCELLABLE = new Set(["download", "verify", "smoke", "waiting"])

// The daemon's refusal (StartGhrUpdate), shown before asking it.
const NOT_A_SERVICE = "ghr is not running as ghr.service; upgrade with setup.sh"

// Why Check now cannot run, or "" when it can.
function checkBlocked(jobs: JobStatus[] | undefined): string {
  const checks = (jobs ?? []).filter((j) => CHECKS.has(j.name))
  if (checks.length === 0) return "This daemon cannot check on request. Update ghr with setup.sh."
  if (checks.every((j) => !j.enabled)) return "Both checks are off. Turn one on under Jobs."
  if (checks.some((j) => j.running || j.pending)) return "A check is running"
  return ""
}

// The install waits for the work a restart would cut, which the restart
// block lists; its own entry is not part of that.
function waitsFor(st: Status): string {
  const work = (st.restart.blocked_by ?? []).filter((w) => w !== "installing ghr")
  return work.length > 0 ? `Waits for: ${work.join(", ")}` : "Waiting for running work to finish"
}

function GhrLine({ g, st, now }: { g: GhrUpdate; st: Status; now: number }) {
  const version = <span className="font-mono text-sm">{g.newer ? `${g.installed} to ${g.latest ?? ""}` : g.installed}</span>
  const note = (text: string) => <span className="text-sm text-muted-foreground">{text}</span>
  if (g.installing && g.step === "waiting") {
    return (
      <>
        {version}
        {g.restart_pending ? <StatusPill state="waiting" label="Installed, waiting to restart" /> : <StatusPill state="running" label="Installing" />}
        {note(waitsFor(st))}
      </>
    )
  }
  if ((g.installing && g.step === "restart") || (!g.installing && g.restart_pending && g.last_outcome !== "failed")) {
    return (
      <>
        {version}
        <Spinner />
        <span className="text-sm">{`Restarting into ${g.restart_pending ?? g.latest ?? ""}`}</span>
      </>
    )
  }
  if (g.installing) {
    return (
      <>
        {version}
        <StatusPill state="running" label="Installing" />
        {note(STEPS[g.step ?? ""] ?? "Starting")}
      </>
    )
  }
  if (g.restart_pending) {
    return (
      <>
        {version}
        <StatusPill state="failed" label="Restart failed" />
      </>
    )
  }
  if (g.development) return <>{version}{note(g.latest ? `Development build. The newest release is ${g.latest}.` : "Development build")}</>
  if (g.newer) {
    return (
      <>
        {version}
        <StatusPill state="waiting" label="Update available" />
      </>
    )
  }
  if (g.checked_at) {
    return (
      <>
        {version}
        <StatusPill state="up to date" />
        {note(`checked ${ago(now - Date.parse(g.checked_at))}`)}
      </>
    )
  }
  if (!g.check_error) {
    return (
      <>
        {version}
        <Spinner label="Checking" />
      </>
    )
  }
  return version
}

function GhrCard({ st, offline }: { st: Status; offline: boolean }) {
  const queryClient = useQueryClient()
  const now = useNow()
  const [confirm, setConfirm] = useState<Confirm | null>(null)
  const g = st.ghr_update
  const refresh = () => queryClient.invalidateQueries({ queryKey: keys.status })
  const install = useMutation({
    mutationFn: () => api.installGhr(),
    onSuccess: () => toast.success(`Installing ghr ${g?.latest ?? ""}`),
    onError: (err) => toast.error(`Install not started: ${errorText(err)}`),
    onSettled: refresh,
  })
  const cancel = useMutation({
    mutationFn: () => api.cancelGhrInstall(),
    onSuccess: () => toast.success("Cancelling the install"),
    onError: (err) => toast.error(`Not cancelled: ${errorText(err)}`),
    onSettled: refresh,
  })
  if (!g) {
    return (
      <FieldGroup id="ghr" title="ghr">
        <p className="py-3 text-sm text-muted-foreground">This daemon does not report its version. Update ghr with setup.sh.</p>
      </FieldGroup>
    )
  }
  const canInstall = g.newer === true && !g.installing && !g.restart_pending
  const canCancel = g.installing === true && CANCELLABLE.has(g.step ?? "") && !g.restart_pending
  const unavailable = st.restart.available ? "" : NOT_A_SERVICE
  const ask = () =>
    setConfirm({
      title: `Install ghr ${g.latest ?? ""}`,
      body: `ghr restarts to use ${g.latest ?? ""}. Running jobs keep running.`,
      action: "Install",
      run: () => install.mutate(),
    })
  return (
    <FieldGroup id="ghr" title="ghr" note="These act at once, outside the save bar.">
      <Field label="ghr version">
        <GhrLine g={g} st={st} now={now} />
      </Field>
      {g.check_error && <p className="py-3 text-sm text-muted-foreground">{`Last check failed: ${g.check_error}`}</p>}
      {g.last_outcome === "failed" && g.last_error && <p className="py-3 text-sm text-destructive">{`Last install failed: ${g.last_error}`}</p>}
      {g.last_outcome === "cancelled" && <p className="py-3 text-sm text-muted-foreground">Last install cancelled</p>}
      {g.last_outcome === "ok" && <p className="py-3 text-sm text-muted-foreground">{`Updated to ${g.installed}`}</p>}
      {!g.installing && g.restart_pending && g.last_outcome === "failed" && (
        <div className="flex flex-wrap items-center gap-3 py-3 text-sm">
          <RestartControl restart={st.restart} offline={offline} />
        </div>
      )}
      {(canInstall || canCancel) && (
        <div className="flex flex-wrap items-center gap-3 py-3">
          {canInstall && (
            <>
              <Button disabled={unavailable !== "" || offline || install.isPending} onClick={ask}>
                {`Install ${g.latest ?? ""}`}
              </Button>
              {unavailable && <span className="text-sm text-muted-foreground">{unavailable}</span>}
            </>
          )}
          {canCancel && (
            <Button variant="outline" disabled={offline || cancel.isPending} onClick={() => cancel.mutate()}>
              Cancel install
            </Button>
          )}
        </div>
      )}
      <ConfirmDialog confirm={confirm} onClose={() => setConfirm(null)} />
    </FieldGroup>
  )
}

export function UpdatesSection() {
  const status = useStatus()
  const queryClient = useQueryClient()
  const st = status.data
  const offline = status.isError
  const check = useMutation({
    mutationFn: async () => {
      for (const j of st?.jobs ?? []) if (CHECKS.has(j.name) && j.enabled) await api.runJob(j.name)
    },
    onSuccess: () => toast.success("Checking for updates"),
    onError: (err) => toast.error(`Check not started: ${errorText(err)}`),
    onSettled: () => queryClient.invalidateQueries({ queryKey: keys.status }),
  })
  if (!st) return <Spinner label="Loading" />
  const blocked = checkBlocked(st.jobs)
  return (
    <div className="flex min-w-0 flex-col gap-6">
      <div className="flex flex-wrap items-center gap-3">
        <Button variant="outline" disabled={blocked !== "" || offline || check.isPending} onClick={() => check.mutate()}>
          Check now
        </Button>
        {blocked && <span className="text-sm text-muted-foreground">{blocked}</span>}
      </div>
      <GhrCard st={st} offline={offline} />
      <RunnerSection />
      <p className="text-sm">
        <Link to="/jobs" className="underline">
          Check schedules are on the Jobs page
        </Link>
      </p>
    </div>
  )
}
