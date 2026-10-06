import { useQueryClient } from "@tanstack/react-query"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "darkraise-ui/components/card"
import { Input } from "darkraise-ui/components/input"
import { Label } from "darkraise-ui/components/label"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "darkraise-ui/components/select"
import { toast } from "darkraise-ui/components/sonner"
import { PageHeader } from "darkraise-ui/layout"
import { useState, type ReactNode } from "react"
import { ApiError, api } from "@/api/client"
import { keys, useConfig, useStatus } from "@/api/hooks"
import type { Config, ConfigPatch, RunnerLimitsPatch } from "@/api/types"
import { AccountCard } from "@/components/account-card"
import { MaintenanceCard } from "@/components/maintenance-card"
import { RejectedAlert, SaveBar } from "@/components/save-bar"
import { TagField } from "@/components/tag-field"
import { TokenCard } from "@/components/token-card"
import { UnsavedGuard } from "@/components/unsaved-guard"
import { changedKeys, sameValue, settle, type Equal, type Values } from "@/lib/draft"
import { DAY_MS, durationError, sameDuration } from "@/lib/duration"
import { errorText } from "@/query"

const MODES: Record<string, string> = {
  queue: "start runners only for queued jobs, up to the global max",
  all: "keep warm runners per repo, up to each repo's max",
}

const FLOORS: Record<string, number> = { poll_interval: 5000, start_timeout: 0, idle_timeout: 0, history_retention: DAY_MS }

function loadedValues(c: Config): Values {
  return {
    mode: c.mode,
    global_max: c.global_max,
    poll_interval: c.poll_interval,
    start_timeout: c.start_timeout,
    idle_timeout: c.idle_timeout,
    disk_high_water: c.disk_high_water,
    build_cache_keep: c.build_cache_keep,
    history_retention: c.history_retention,
    labels: c.labels ?? [],
    memory_max: c.runner_limits.memory_max,
    cpu_quota: c.runner_limits.cpu_quota,
  }
}

const equal: Equal = (key, a, b) => {
  if (key in FLOORS) return sameDuration(String(a), String(b))
  return typeof a === "string" && typeof b === "string" ? a.trim() === b.trim() : sameValue(a, b)
}

function fieldErrors(v: Values): Record<string, string> {
  const errors: Record<string, string> = {}
  for (const [key, floor] of Object.entries(FLOORS)) {
    const err = durationError(key, String(v[key] ?? ""), floor)
    if (err) errors[key] = err
  }
  const max = Number(v.global_max)
  if (!Number.isInteger(max) || max < 1) errors.global_max = "global_max must be >= 1"
  const high = Number(v.disk_high_water)
  if (!Number.isInteger(high) || high < 1 || high > 100) errors.disk_high_water = "disk_high_water must be 1..100"
  return errors
}

const isLimit = (key: string): key is keyof RunnerLimitsPatch => key === "memory_max" || key === "cpu_quota"

function toPatch(sent: Values): ConfigPatch {
  const patch: Record<string, unknown> = {}
  const limits: RunnerLimitsPatch = {}
  for (const [key, v] of Object.entries(sent)) {
    const value = typeof v === "string" ? v.trim() : v
    if (isLimit(key)) limits[key] = value as string
    else patch[key] = value
  }
  if (Object.keys(limits).length > 0) patch.runner_limits = limits
  return patch as ConfigPatch
}

// The daemon took the save, so until a read succeeds the cached config shows
// what was sent; the next poll replaces it with what the daemon holds.
function withPatch(c: Config, patch: ConfigPatch): Config {
  const { runner_limits: limits, ...rest } = patch
  return { ...c, ...rest, runner_limits: { ...c.runner_limits, ...limits } } as Config
}

const patchKey = (key: string) => (isLimit(key) ? `runner_limits.${key}` : key)

function Section({ title, note, children }: { title: string; note?: string; children: ReactNode }) {
  return (
    <Card className="mb-4">
      <CardHeader>
        <CardTitle>{title}</CardTitle>
        {note && <CardDescription>{note}</CardDescription>}
      </CardHeader>
      <CardContent className="flex flex-col gap-4">{children}</CardContent>
    </Card>
  )
}

function Field({
  label,
  htmlFor,
  desc,
  changed,
  error,
  children,
}: {
  label: string
  htmlFor?: string
  desc: string
  changed?: boolean
  error?: string
  children: ReactNode
}) {
  return (
    <div className="grid gap-1 sm:grid-cols-[12rem_1fr] sm:items-start">
      <Label htmlFor={htmlFor} className="sm:pt-2">
        {label}
      </Label>
      <div className="flex flex-col gap-1">
        <div className="flex items-center gap-2">
          {children}
          {changed && (
            <span className="text-amber-600" title="changed">
              ●
            </span>
          )}
        </div>
        {error && <p className="text-sm text-destructive">✖ {error}</p>}
        <p className="text-xs text-muted-foreground">{desc}</p>
      </div>
    </div>
  )
}

export function SettingsPage() {
  const config = useConfig()
  const status = useStatus()
  const queryClient = useQueryClient()
  const [draft, setDraft] = useState<Values>({})
  const [saving, setSaving] = useState(false)
  const [rejection, setRejection] = useState("")

  const loaded = config.data ? loadedValues(config.data) : {}
  const values: Values = { ...loaded, ...draft }
  const changed = config.data ? changedKeys(draft, loaded, equal) : []
  const errors = config.data ? fieldErrors(values) : {}
  const offline = status.isError

  const set = (key: string) => (value: unknown) => setDraft((d) => ({ ...d, [key]: value }))
  const text = (key: string) => String(values[key] ?? "")
  const num = (key: string) => (Number.isNaN(values[key]) ? "" : String(values[key]))
  const toNum = (s: string) => (s === "" ? Number.NaN : Number(s))

  function discard() {
    setDraft({})
    setRejection("")
  }

  async function save(): Promise<boolean> {
    if (changed.length === 0) return true
    if (saving) return false
    if (Object.keys(errors).length > 0) {
      toast.error("fix the highlighted settings first")
      return false
    }
    const sent = Object.fromEntries(changed.map((k) => [k, values[k]]))
    setSaving(true)
    setRejection("")
    try {
      await api.patchConfig(toPatch(sent))
    } catch (err) {
      setSaving(false)
      if (err instanceof ApiError && err.status >= 400 && err.status < 500) {
        setRejection(err.message)
        toast.error("settings not saved")
      } else {
        toast.error(`settings not saved: ${errorText(err)}`)
      }
      return false
    }
    toast.success("Settings saved")
    try {
      const fresh = loadedValues(
        await queryClient.fetchQuery({ queryKey: keys.config, queryFn: ({ signal }) => api.config(signal), staleTime: 0 }),
      )
      const missed = Object.keys(sent).find((key) => !equal(key, fresh[key], sent[key]))
      if (missed) toast.error(`daemon did not apply ${patchKey(missed)}; is it older than this ghr?`)
    } catch (err) {
      toast.error(`saved, but re-reading the config failed: ${errorText(err)}`)
      queryClient.setQueryData<Config>(keys.config, (c) => c && withPatch(c, toPatch(sent)))
    }
    setDraft((d) => settle(d, sent, equal))
    setSaving(false)
    return true
  }

  const is = (key: string) => changed.includes(key)

  function duration(key: string, label: string, desc: string) {
    return (
      <Field label={label} htmlFor={key} desc={desc} changed={is(key)} error={errors[key]}>
        <Input id={key} className="w-40" value={text(key)} disabled={offline} onChange={(e) => set(key)(e.target.value)} />
      </Field>
    )
  }

  function plain(key: string, label: string, desc: string) {
    return (
      <Field label={label} htmlFor={key} desc={desc} changed={is(key)}>
        <Input id={key} className="w-40" value={text(key)} disabled={offline} onChange={(e) => set(key)(e.target.value)} />
      </Field>
    )
  }

  return (
    <>
      <PageHeader title="Settings" />
      {!config.data ? (
        <p className="text-sm text-muted-foreground">loading…</p>
      ) : (
        <>
          {rejection && <RejectedAlert message={rejection} />}
          <Section title="General">
            <Field label="Mode" desc={MODES[text("mode")] ?? ""} changed={is("mode")}>
              <Select value={text("mode")} onValueChange={set("mode")} disabled={offline}>
                <SelectTrigger aria-label="Mode" className="w-40">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {Object.keys(MODES).map((m) => (
                    <SelectItem key={m} value={m}>
                      {m}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </Field>
            <Field label="Global max" htmlFor="global_max" desc="applies in queue mode" changed={is("global_max")} error={errors.global_max}>
              <Input
                id="global_max"
                type="number"
                min={1}
                max={99}
                step={1}
                className="w-24"
                value={num("global_max")}
                disabled={offline}
                onChange={(e) => set("global_max")(toNum(e.target.value))}
              />
            </Field>
            <Field label="Owner" desc="change in config.yaml and restart the daemon">
              <span className="font-mono text-sm">{config.data.owner}</span>
            </Field>
          </Section>
          <Section title="Timing">
            {duration("poll_interval", "Poll interval", "how often GitHub is checked (at least 5s)")}
            {duration("start_timeout", "Start timeout", "a runner not online by then is replaced")}
            {duration("idle_timeout", "Idle timeout", "idle runners beyond warm stop after this")}
          </Section>
          <Section title="Disk and retention">
            <Field
              label="Disk high-water"
              htmlFor="disk_high_water"
              desc="disk use that triggers pruning"
              changed={is("disk_high_water")}
              error={errors.disk_high_water}
            >
              <Input
                id="disk_high_water"
                type="number"
                min={1}
                max={100}
                step={5}
                suffix="%"
                className="w-24"
                value={num("disk_high_water")}
                disabled={offline}
                onChange={(e) => set("disk_high_water")(toNum(e.target.value))}
              />
            </Field>
            {plain("build_cache_keep", "Build cache keep", "build cache kept when pruning, e.g. 20GB")}
            {duration("history_retention", "History retention", "history and logs older than this are removed (at least 1d)")}
          </Section>
          <Section title="Runner defaults" note="limits apply to newly started runners">
            <Field label="Global labels" desc="added to every runner" changed={is("labels")}>
              <TagField label="Global labels" value={values.labels as string[]} onChange={set("labels")} disabled={offline} />
            </Field>
            {plain("memory_max", "Memory max", "per runner, e.g. 6G, 50% or infinity")}
            {plain("cpu_quota", "CPU quota", "per runner, e.g. 200%")}
          </Section>
          <div className="mb-4 grid gap-4 lg:grid-cols-2">
            <TokenCard />
            <MaintenanceCard />
            <AccountCard />
          </div>
          <SaveBar count={changed.length} saving={saving} disabled={offline} onSave={() => void save()} onDiscard={discard} />
          <UnsavedGuard count={changed.length} page="Settings" saving={saving} onSave={save} onDiscard={discard} />
        </>
      )}
    </>
  )
}
