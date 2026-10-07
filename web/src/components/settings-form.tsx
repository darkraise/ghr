import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "darkraise-ui/components/card"
import { Input } from "darkraise-ui/components/input"
import { Label } from "darkraise-ui/components/label"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "darkraise-ui/components/select"
import type { ReactNode } from "react"
import { RejectedAlert } from "@/components/save-bar"
import { TagField } from "@/components/tag-field"
import type { SettingsForm } from "@/lib/settings-form"

const MODES: Record<string, string> = {
  queue: "start runners only for queued jobs, up to the global max",
  all: "keep warm runners per repo, up to each repo's max",
}

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

export function SettingsSections({ form }: { form: SettingsForm }) {
  const { values, changed, errors, offline, set } = form
  const text = (key: string) => String(values[key] ?? "")
  const num = (key: string) => (Number.isNaN(values[key]) ? "" : String(values[key]))
  const toNum = (s: string) => (s === "" ? Number.NaN : Number(s))
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
      {form.rejection && <RejectedAlert message={form.rejection} />}
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
          <span className="font-mono text-sm">{form.config?.owner}</span>
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
    </>
  )
}
