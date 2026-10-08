import { useMutation, useQueryClient } from "@tanstack/react-query"
import { Button } from "darkraise-ui/components/button"
import { toast } from "darkraise-ui/components/sonner"
import { Spinner } from "darkraise-ui/components/spinner"
import { TriangleAlert } from "lucide-react"
import { useId } from "react"
import { api } from "@/api/client"
import { keys } from "@/api/hooks"
import type { RunnerUpdate } from "@/api/types"
import { DAY_MS } from "@/lib/duration"
import { hhmm, monthDay, plural } from "@/lib/format"
import { useNow } from "@/lib/use-now"

type UpdateState = "hidden" | "running" | "queued" | "failed" | "due"

function updateState(u: RunnerUpdate): UpdateState {
  if (u.running) return "running"
  if (u.queued) return "queued"
  if (!u.deadline) return "hidden"
  return u.last_outcome === "failed" ? "failed" : "due"
}

function deadlineText(u: RunnerUpdate, now: number): string {
  const deadline = Date.parse(u.deadline ?? "")
  const left = deadline - now
  const when = left <= 0 ? "overdue" : left < DAY_MS ? "less than a day left" : `${plural(Math.floor(left / DAY_MS), "day")} left`
  return `${u.latest ?? "A newer runner"} required by ${monthDay(new Date(deadline))}, ${when}`
}

export function UpdateCard({ update, offline }: { update: RunnerUpdate; offline: boolean }) {
  const queryClient = useQueryClient()
  const now = useNow()
  const titleId = useId()
  const act = useMutation({
    mutationFn: (cancel: boolean) => (cancel ? api.cancelRunnerUpdate() : api.queueRunnerUpdate()),
    onSuccess: (_data, cancel) => {
      toast.success(cancel ? "Runner update cancelled" : "Runner update queued")
    },
    onSettled: () => queryClient.invalidateQueries({ queryKey: keys.status }),
  })
  const state = updateState(update)
  if (state === "hidden") return null
  const urgent = update.deadline !== undefined && Date.parse(update.deadline) - now <= 7 * DAY_MS
  const locked = offline || act.isPending
  const queuedAt = update.queued_at ? new Date(update.queued_at) : new Date(now)
  return (
    <section
      aria-labelledby={titleId}
      className={`flex flex-col items-start gap-2 rounded-[10px] border bg-card p-3 text-sm text-card-foreground ${urgent ? "border-destructive" : "border-warning"}`}
    >
      <h2 id={titleId} className={`flex items-center gap-1.5 font-semibold ${urgent ? "text-destructive" : "text-warning"}`}>
        <TriangleAlert size={15} aria-hidden="true" />
        Runner update
      </h2>
      {state === "running" && (
        <p className="flex items-center gap-2">
          <Spinner />
          <span>Updating runners</span>
        </p>
      )}
      {state === "queued" && <p>{`Queued since ${hhmm(queuedAt)}. Runners update between jobs.`}</p>}
      {(state === "due" || state === "failed") && <p>{deadlineText(update, now)}</p>}
      {state === "failed" && update.last_error && <p className="text-destructive">{update.last_error}</p>}
      {state === "queued" && (
        <Button size="sm" variant="outline" disabled={locked} onClick={() => act.mutate(true)}>
          Cancel update
        </Button>
      )}
      {state === "due" && (
        <Button size="sm" disabled={locked} onClick={() => act.mutate(false)}>
          Queue update
        </Button>
      )}
      {state === "failed" && (
        <Button size="sm" disabled={locked} onClick={() => act.mutate(false)}>
          Try again
        </Button>
      )}
    </section>
  )
}
