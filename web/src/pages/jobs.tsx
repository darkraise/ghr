import { useMutation, useQueryClient } from "@tanstack/react-query"
import { Button } from "darkraise-ui/components/button"
import { toast } from "darkraise-ui/components/sonner"
import { Spinner } from "darkraise-ui/components/spinner"
import { Switch } from "darkraise-ui/components/switch"
import { PageHeader } from "darkraise-ui/layout"
import type { ReactNode } from "react"
import { api } from "@/api/client"
import { keys, useStatus } from "@/api/hooks"
import type { JobStatus } from "@/api/types"
import { NumberField } from "@/components/number-field"
import { ErrorLine } from "@/components/page/error-line"
import { Field, FieldGroup } from "@/components/page/field"
import { StatusPill } from "@/components/page/status-pill"
import { RejectedAlert, SaveBar } from "@/components/save-bar"
import { UnsavedGuard } from "@/components/unsaved-guard"
import { FIELD_WIDTH } from "@/lib/field-width"
import { ago, dur } from "@/lib/format"
import { JOB_KEYS, useSettingsForm, type SettingsForm } from "@/lib/settings-form"
import { UNIT_SETTINGS } from "@/lib/units"
import { useNow } from "@/lib/use-now"
import { errorText } from "@/query"

// What turning a task off costs, in the words of config.RunnerCheckOffReason
// and config.PruneOffReason, which config.Validate warns with; change both
// together.
// eslint-disable-next-line react-refresh/only-export-components -- a text table, exported for its test
export const OFF_WARNINGS: Record<string, string> = {
  runner_update_check:
    "GitHub stops assigning jobs to a runner past its forced-update deadline, and ghr no longer learns of new runner releases or deadlines",
  prune: "history and logs older than history_retention are kept, and a full disk is no longer pruned automatically",
}

const sentence = (s: string) => `${s.charAt(0).toUpperCase()}${s.slice(1)}.`

function lastRun(j: JobStatus, now: number): ReactNode {
  if (j.running) return <StatusPill state="running" />
  if (!j.last_finish) return <span className="text-sm text-muted-foreground">Never run</span>
  const when = ago(now - Date.parse(j.last_finish))
  if (j.last_error) return <span className="text-sm text-destructive">{`Failed ${when}: ${j.last_error}`}</span>
  return <span className="text-sm">{`Ran ${when}`}</span>
}

function nextRun(j: JobStatus, now: number): string {
  if (!j.enabled) return "Off"
  if (j.pending) return "Starting"
  if (j.running || !j.next) return "After this run"
  const left = Date.parse(j.next) - now
  return left <= 0 ? "Due" : `In ${dur(left)}`
}

// Why Run now is unavailable, or "" when it can run.
function blocked(j: JobStatus): string {
  if (!j.enabled) return "Turn it on and save to run it"
  if (j.running) return "Running now"
  if (j.pending) return "Starts at the next pass"
  return ""
}

function JobGroup({ job: j, form, now, onRun, running }: { job: JobStatus; form: SettingsForm; now: number; onRun: () => void; running: boolean }) {
  const { values, changed, errors, rejectedFields, offline, set } = form
  const onKey = `jobs.${j.name}.enabled`
  const everyKey = `jobs.${j.name}.every`
  const on = values[onKey] === true
  const every = values[everyKey]
  const unit = UNIT_SETTINGS[everyKey]
  const warning = !on && OFF_WARNINGS[j.name] ? sentence(OFF_WARNINGS[j.name] ?? "") : undefined
  const reason = blocked(j)
  return (
    <FieldGroup id={`job-${j.name}`} title={j.title} note={j.description}>
      <Field label="Automatic" help={warning} changed={changed.includes(onKey)}>
        <Switch aria-label={`Run ${j.title} automatically`} checked={on} disabled={offline} onCheckedChange={set(onKey)} />
      </Field>
      <Field label="Every" htmlFor={everyKey} changed={changed.includes(everyKey)} error={errors[everyKey] ?? rejectedFields[everyKey]}>
        <NumberField
          id={everyKey}
          label={`${j.title} every`}
          min={0}
          suffix={unit?.unit}
          spokenSuffix={unit?.spoken}
          className={FIELD_WIDTH}
          placeholder={typeof every === "string" ? every : ""}
          value={typeof every === "number" ? every : Number.NaN}
          disabled={offline}
          onValueChange={set(everyKey)}
        />
      </Field>
      <Field label="Last run">{lastRun(j, now)}</Field>
      <Field label="Next run" help={reason || undefined}>
        <span className="text-sm">{nextRun(j, now)}</span>
        <Button size="sm" variant="outline" disabled={reason !== "" || offline || running} onClick={onRun}>
          Run now
        </Button>
      </Field>
    </FieldGroup>
  )
}

export function JobsPage() {
  const status = useStatus()
  const form = useSettingsForm(JOB_KEYS)
  const queryClient = useQueryClient()
  const now = useNow()
  const run = useMutation({
    mutationFn: (j: JobStatus) => api.runJob(j.name),
    onSuccess: (_data, j) => toast.success(`${j.title} started`),
    onError: (err) => toast.error(`Not started: ${errorText(err)}`),
    onSettled: () => queryClient.invalidateQueries({ queryKey: keys.status }),
  })
  const jobs = status.data?.jobs

  let body: ReactNode
  if (status.data && !jobs) {
    body = <p className="text-sm text-muted-foreground">This daemon reports no background tasks. Update ghr to manage them here.</p>
  } else if (!jobs || !form.config) {
    body = form.configError ? <ErrorLine onRetry={form.retryConfig}>{form.configError}</ErrorLine> : <Spinner label="Loading" />
  } else {
    body = (
      <>
        <div className="flex min-w-0 flex-col gap-6">
          {form.rejection && <RejectedAlert message={form.rejection} />}
          {jobs.map((j) => (
            <JobGroup key={j.name} job={j} form={form} now={now} running={run.isPending} onRun={() => run.mutate(j)} />
          ))}
        </div>
        <SaveBar count={form.changed.length} saving={form.saving} disabled={form.offline} onSave={() => void form.save()} onDiscard={form.discard} />
        <UnsavedGuard count={form.changed.length} page="Jobs" saving={form.saving} onSave={form.save} onDiscard={form.discard} />
      </>
    )
  }
  return (
    <>
      <PageHeader title="Jobs" description="Tasks ghr runs on its own schedule. Changes save to config.yaml and apply at once." />
      {body}
    </>
  )
}
