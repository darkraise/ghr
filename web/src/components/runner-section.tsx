import { useMutation, useQueryClient } from "@tanstack/react-query"
import { Button } from "darkraise-ui/components/button"
import { toast } from "darkraise-ui/components/sonner"
import { Spinner } from "darkraise-ui/components/spinner"
import { useState } from "react"
import { api } from "@/api/client"
import { keys, useStatus } from "@/api/hooks"
import type { RunnerUpdate } from "@/api/types"
import { Field, FieldGroup } from "@/components/page/field"
import { StateText } from "@/components/page/state-text"
import { DAY_MS } from "@/lib/duration"
import { ago, monthDay, plural } from "@/lib/format"
import { useNow } from "@/lib/use-now"
import { errorText } from "@/query"

function reloadText(warnings: number): string {
  if (warnings === 0) return "Config reloaded"
  return `Config reloaded with ${plural(warnings, "warning")}. See Events on the Dashboard.`
}

function deadlineText(deadline: string, now: number): string {
  const left = Date.parse(deadline) - now
  const when = left <= 0 ? "overdue" : left < DAY_MS ? "in less than a day" : `in ${plural(Math.floor(left / DAY_MS), "day")}`
  return `Required by ${monthDay(new Date(deadline))}, ${when}`
}

// The states are checked in the shell update card's order, so the two never
// disagree about what the runner is doing.
function VersionLine({ u, now }: { u: RunnerUpdate; now: number }) {
  if (!u.installed) return <span className="text-sm">Version unknown. No dist/current link was found.</span>
  const versions = <span className="font-mono text-sm">{`${u.installed} to ${u.latest ?? ""}`}</span>
  if (u.running) {
    return (
      <>
        {versions}
        <StateText state="running" label="Updating" />
      </>
    )
  }
  if (u.queued) {
    return (
      <>
        {versions}
        <StateText state="queued" />
      </>
    )
  }
  if (u.deadline) {
    const urgent = Date.parse(u.deadline) - now <= 7 * DAY_MS
    return (
      <>
        {versions}
        <StateText state={urgent ? "failed" : "waiting"} label="Update required" />
        <span className="text-sm text-muted-foreground">{deadlineText(u.deadline, now)}</span>
      </>
    )
  }
  const installed = <span className="font-mono text-sm">{u.installed}</span>
  if (u.checked_at) {
    return (
      <>
        {installed}
        <StateText state="up to date" />
        <span className="text-sm text-muted-foreground">{`checked ${ago(now - Date.parse(u.checked_at))}`}</span>
      </>
    )
  }
  if (!u.check_error) {
    return (
      <>
        {installed}
        <Spinner label="Checking" />
      </>
    )
  }
  return installed
}

export function RunnerSection() {
  const status = useStatus()
  const queryClient = useQueryClient()
  const now = useNow()
  const [reloading, setReloading] = useState(false)
  const offline = status.isError
  const u = status.data?.runner_update

  const refresh = () =>
    Promise.all([keys.status, ["events"], keys.config, keys.token].map((queryKey) => queryClient.invalidateQueries({ queryKey })))

  const update = useMutation({
    mutationFn: (cancel: boolean) => (cancel ? api.cancelRunnerUpdate() : api.queueRunnerUpdate()),
    onSuccess: (_data, cancel) => {
      toast.success(cancel ? "Runner update cancelled" : "Runner update queued")
    },
    onSettled: () => refresh(),
  })

  async function reload() {
    setReloading(true)
    try {
      toast.success(reloadText((await api.reload()).length))
    } catch (err) {
      toast.error(`Reload rejected: ${errorText(err)}`)
    } finally {
      setReloading(false)
      await refresh()
    }
  }

  const canQueue = u?.deadline !== undefined && !u.running && !u.queued
  return (
    <FieldGroup id="runner" title="Runner and config" note="These act at once, outside the save bar.">
      <Field label="Runner version" help={u?.queued ? "Runs when no job is running or queued" : undefined}>
        {u ? <VersionLine u={u} now={now} /> : <Spinner label="Loading" />}
      </Field>
      {u?.last_outcome === "failed" && u.last_error && <p className="py-3 text-sm text-destructive">{`Last update failed: ${u.last_error}`}</p>}
      {u?.check_error && <p className="py-3 text-sm text-muted-foreground">{`Last check failed: ${u.check_error}`}</p>}
      {(u?.queued || canQueue) && (
        <div className="py-3">
          {u?.queued ? (
            <Button variant="outline" disabled={offline || update.isPending} onClick={() => update.mutate(true)}>
              Cancel update
            </Button>
          ) : (
            <Button disabled={offline || update.isPending} onClick={() => update.mutate(false)}>
              Queue update
            </Button>
          )}
        </div>
      )}
      <Field label="Config" help="Reads config.yaml again. Warnings appear under Events on the Dashboard.">
        <Button variant="outline" disabled={offline} loading={reloading} onClick={() => void reload()}>
          Reload config.yaml
        </Button>
      </Field>
    </FieldGroup>
  )
}
