import { useMutation, useQueryClient } from "@tanstack/react-query"
import { Button } from "darkraise-ui/components/button"
import { Input } from "darkraise-ui/components/input"
import { toast } from "darkraise-ui/components/sonner"
import { useState } from "react"
import { ApiError, api } from "@/api/client"
import { keys } from "@/api/hooks"
import type { Config, GitHubStorage, GitHubStoragePatch, StoragePreview } from "@/api/types"
import { CleanupPreview } from "@/components/cleanup-preview"
import { ConfirmDialog, type Confirm } from "@/components/confirm-dialog"
import { ErrorLine } from "@/components/page/error-line"
import { Field, FieldGroup } from "@/components/page/field"
import { FIELD_WIDTH } from "@/lib/field-width"
import { CLEANUP_WARNING, RETENTION_WARNING } from "@/lib/quota"
import { errorText } from "@/query"

const repositories = (n: number) => (n === 1 ? "1 repository" : `${n} repositories`)

interface Draft {
  included: string
  warn: string
  critical: string
  age: string
}

const whole = (s: string) => (/^\d+$/.test(s.trim()) ? Number(s) : Number.NaN)

function saved(config: Config | undefined): Draft {
  const g = config?.github_storage
  const [warn, critical] = g?.warn_pct ?? [70, 90]
  return { included: String(g?.included_mb ?? 500), warn: String(warn), critical: String(critical), age: g?.cleanup_max_age ?? "" }
}

function problems(d: Draft): Partial<Record<keyof Draft, string>> {
  const out: Partial<Record<keyof Draft, string>> = {}
  if (!(whole(d.included) >= 1)) out.included = "Enter a whole number of MB, at least 1"
  const w = whole(d.warn)
  const c = whole(d.critical)
  if (!(w >= 1 && c <= 100 && w < c)) out.critical = "Enter two whole percentages from 1 to 100, the first lower"
  return out
}

export function StoragePolicyCard({
  storage,
  config,
  offline,
  busy,
}: {
  storage: GitHubStorage
  config: Config | undefined
  offline: boolean
  busy: boolean
}) {
  const queryClient = useQueryClient()
  const [draft, setDraft] = useState<Partial<Draft>>({})
  const [confirm, setConfirm] = useState<Confirm | null>(null)
  const [rejection, setRejection] = useState("")
  const [saving, setSaving] = useState(false)
  const [days, setDays] = useState("")
  const loaded = saved(config)
  const values: Draft = { ...loaded, ...draft }
  const changed = (Object.keys(values) as (keyof Draft)[]).filter((k) => values[k].trim() !== loaded[k])
  const errors = problems(values)
  const set = (key: keyof Draft) => (e: { target: { value: string } }) => setDraft((d) => ({ ...d, [key]: e.target.value }))

  const retention = useMutation({
    mutationFn: (n: number) => api.setRetentionAll(n),
    onSuccess: () => {
      toast.success("Retention change started")
      setDays("")
    },
    onSettled: () => queryClient.invalidateQueries({ queryKey: keys.githubStorage }),
  })

  async function send(patch: GitHubStoragePatch) {
    setSaving(true)
    setRejection("")
    try {
      await api.patchConfig({ github_storage: patch })
    } catch (err) {
      setSaving(false)
      if (err instanceof ApiError && err.status >= 400 && err.status < 500) {
        setRejection(err.message)
        toast.error("Storage policy not saved")
      } else {
        toast.error(`Storage policy not saved: ${errorText(err)}`)
      }
      return
    }
    setSaving(false)
    setDraft({})
    toast.success("Storage policy saved")
    void queryClient.invalidateQueries({ queryKey: keys.config })
    void queryClient.invalidateQueries({ queryKey: keys.githubStorage })
  }

  async function save() {
    if (changed.length === 0 || saving || Object.keys(errors).length > 0) return
    const patch: GitHubStoragePatch = {}
    if (changed.includes("included")) patch.included_mb = whole(values.included)
    if (changed.includes("warn") || changed.includes("critical")) patch.warn_pct = [whole(values.warn), whole(values.critical)]
    const age = values.age.trim()
    if (!changed.includes("age")) return send(patch)
    patch.cleanup_max_age = age
    if (age === "") return send(patch)
    let previews: StoragePreview[]
    try {
      previews = await api.storagePreview(age)
    } catch (err) {
      setRejection(errorText(err))
      toast.error("Storage policy not saved")
      return
    }
    setConfirm({
      title: `Clean artifacts after ${age}?`,
      body: CLEANUP_WARNING,
      details: <CleanupPreview age={age} previews={previews} />,
      action: "Save",
      destructive: previews.some((p) => p.count > 0),
      run: () => void send(patch),
    })
  }

  const n = whole(days)
  const daysOK = n >= 1 && n <= 400
  const count = storage.repos.length

  return (
    <FieldGroup id="storage-policy" title="Policy" note="Saved in config.yaml as github_storage.">
      {rejection && (
        <div className="py-3">
          <ErrorLine>{rejection}</ErrorLine>
        </div>
      )}
      <Field label="Included storage" htmlFor="included_mb" help="The plan's allowance for artifacts and Packages, in MB." error={errors.included} changed={changed.includes("included")}>
        <Input id="included_mb" type="number" min={1} suffix="MB" groupClassName={FIELD_WIDTH} className="min-w-0" value={values.included} disabled={offline} onChange={set("included")} />
      </Field>
      <Field label="Warning levels" help="Warn, then warn louder, when the estimate reaches these shares of the allowance." error={errors.critical} changed={changed.includes("warn") || changed.includes("critical")}>
        <Input aria-label="Warning at" type="number" min={1} max={100} suffix="%" groupClassName="w-28" className="min-w-0" value={values.warn} disabled={offline} onChange={set("warn")} />
        <Input aria-label="Critical at" type="number" min={1} max={100} suffix="%" groupClassName="w-28" className="min-w-0" value={values.critical} disabled={offline} onChange={set("critical")} />
      </Field>
      <Field label="Cleanup age" htmlFor="cleanup_max_age" help={`Delete artifacts older than this, such as 2d; empty is off, and at least 24h. ${CLEANUP_WARNING}`} changed={changed.includes("age")}>
        <Input id="cleanup_max_age" className={`${FIELD_WIDTH} font-mono`} placeholder="off" value={values.age} disabled={offline} onChange={set("age")} />
      </Field>
      <div className="flex justify-end gap-2 py-3">
        {changed.length > 0 && (
          <Button variant="ghost" disabled={saving} onClick={() => setDraft({})}>
            Discard
          </Button>
        )}
        <Button disabled={offline || saving || changed.length === 0} onClick={() => void save()}>
          Save policy
        </Button>
      </div>
      <Field label="Retention for all" htmlFor="retention_all" help={`Sets GitHub's retention in days on every repository listed below. ${RETENTION_WARNING}`} error={days !== "" && !daysOK ? "Enter a whole number of days from 1 to 400" : undefined}>
        <Input id="retention_all" type="number" min={1} max={400} suffix="days" groupClassName="w-36" className="min-w-0" value={days} disabled={offline} onChange={(e) => setDays(e.target.value)} />
        <Button
          variant="outline"
          disabled={offline || busy || !daysOK || count === 0 || retention.isPending}
          onClick={() =>
            setConfirm({
              title: `Set retention to ${n} days on ${repositories(count)}?`,
              body: `${RETENTION_WARNING} A repository whose maximum is lower is skipped.`,
              action: "Set retention",
              run: () => retention.mutate(n),
            })
          }
        >
          Set retention
        </Button>
      </Field>
      <ConfirmDialog confirm={confirm} onClose={() => setConfirm(null)} />
    </FieldGroup>
  )
}
