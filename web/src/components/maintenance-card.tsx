import { useMutation, useQueryClient } from "@tanstack/react-query"
import { Badge } from "darkraise-ui/components/badge"
import { Button } from "darkraise-ui/components/button"
import { Card, CardContent, CardHeader, CardTitle } from "darkraise-ui/components/card"
import { toast } from "darkraise-ui/components/sonner"
import { useState, type ReactNode } from "react"
import { api } from "@/api/client"
import { keys, useStatus } from "@/api/hooks"
import type { RunnerUpdate } from "@/api/types"
import { DAY_MS } from "@/lib/duration"
import { ago, dateTime } from "@/lib/format"
import { useNow } from "@/lib/use-now"
import { errorText } from "@/query"

function reloadText(warnings: number): string {
  if (warnings === 0) return "config reloaded"
  return warnings === 1 ? "config reloaded (1 warning, see Activity)" : `config reloaded (${warnings} warnings, see Activity)`
}

function RunnerVersion({ u, now }: { u: RunnerUpdate; now: number }) {
  const versions = `${u.installed} → ${u.latest}`
  let main: ReactNode = u.installed
  let note = ""
  if (!u.installed) {
    main = <span className="text-muted-foreground">version unknown (no dist/current)</span>
  } else if (u.running) {
    main = (
      <>
        <span>{versions}</span>
        <Badge variant="blue" size="sm">
          updating
        </Badge>
      </>
    )
  } else if (u.queued) {
    main = (
      <>
        <span>{versions}</span>
        <Badge variant="amber" size="sm">
          queued
        </Badge>
      </>
    )
    note = "runs when no job is running or queued"
  } else if (u.deadline) {
    const left = Date.parse(u.deadline) - now
    main = (
      <>
        <span>{versions}</span>
        <Badge variant={left <= 7 * DAY_MS ? "red" : "amber"} size="sm">
          update available
        </Badge>
      </>
    )
    note = `update by ${dateTime(u.deadline).slice(0, 10)} (${left <= 0 ? "overdue" : `${Math.floor(left / DAY_MS)} days`})`
  } else if (u.checked_at) {
    main = (
      <>
        <span>{u.installed}</span>
        <Badge variant="green" size="sm">
          up to date
        </Badge>
        <span className="text-muted-foreground">checked {ago(now - Date.parse(u.checked_at))}</span>
      </>
    )
  } else if (!u.check_error) {
    main = <span>{u.installed} checking…</span>
  }
  return (
    <div className="flex flex-col gap-1 text-sm">
      <div className="flex flex-wrap items-center gap-2">
        <span className="w-24 text-muted-foreground">Runner</span>
        {main}
      </div>
      {note && <p className="text-muted-foreground">{note}</p>}
      {u.check_error && <p className="text-muted-foreground">last check failed: {u.check_error}</p>}
    </div>
  )
}

export function MaintenanceCard() {
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
      toast.success(cancel ? "queued runner update cancelled" : "runner update queued")
    },
    onSettled: () => refresh(),
  })

  async function reload() {
    setReloading(true)
    try {
      toast.success(reloadText((await api.reload()).length))
    } catch (err) {
      toast.error(`reload rejected: ${errorText(err)}`)
    } finally {
      setReloading(false)
      await refresh()
    }
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>Maintenance</CardTitle>
      </CardHeader>
      <CardContent className="flex flex-col gap-3">
        {u ? <RunnerVersion u={u} now={now} /> : <p className="text-sm text-muted-foreground">loading…</p>}
        <div className="flex flex-wrap gap-2">
          {u?.queued && (
            <Button variant="secondary" disabled={offline || update.isPending} onClick={() => update.mutate(true)}>
              Cancel queued update
            </Button>
          )}
          {u?.deadline && !u.running && !u.queued && (
            <Button disabled={offline || update.isPending} onClick={() => update.mutate(false)}>
              Queue update
            </Button>
          )}
          <Button disabled={offline} loading={reloading} onClick={() => void reload()}>
            Reload config.yaml
          </Button>
        </div>
      </CardContent>
    </Card>
  )
}
