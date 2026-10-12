import { Input } from "darkraise-ui/components/input"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "darkraise-ui/components/select"
import { useStatus } from "@/api/hooks"
import { Field, FieldGroup } from "@/components/page/field"
import { RestartControl } from "@/components/restart-banner"
import { RejectedAlert } from "@/components/save-bar"
import { TagField } from "@/components/tag-field"
import { FIELD_WIDTH } from "@/lib/field-width"
import type { SettingsForm } from "@/lib/settings-form"
import { UNIT_SETTINGS } from "@/lib/units"

const MODES: Record<string, string> = {
  queue: "Start runners only for queued jobs, up to Max concurrent runners",
  all: "Keep warm runners per repo, up to each repo's max",
}

// webAccess: only Settings → General shows the web listener's settings; the
// setup wizard leaves them out.
export function SettingsSections({ form, webAccess = false }: { form: SettingsForm; webAccess?: boolean }) {
  const { values, changed, errors, offline, set } = form
  const restart = useStatus().data?.restart
  const text = (key: string) => String(values[key] ?? "")
  const num = (key: string) => (Number.isNaN(values[key]) ? "" : String(values[key]))
  const toNum = (s: string) => (s === "" ? Number.NaN : Number(s))
  const is = (key: string) => changed.includes(key)

  // A value the unit cannot show stays as the daemon's text, in the placeholder.
  function unitField(key: string, label: string, help: string, empty = "") {
    const v = values[key]
    return (
      <Field label={label} htmlFor={key} help={help} changed={is(key)} error={errors[key]}>
        <Input
          id={key}
          type="number"
          min={0}
          suffix={UNIT_SETTINGS[key]?.unit}
          groupClassName={FIELD_WIDTH}
          className="min-w-0"
          placeholder={typeof v === "string" ? v : empty}
          value={typeof v === "number" && !Number.isNaN(v) ? String(v) : ""}
          disabled={offline}
          onChange={(e) => set(key)(toNum(e.target.value))}
        />
      </Field>
    )
  }

  return (
    <div className="flex flex-col gap-6">
      {form.rejection && <RejectedAlert message={form.rejection} />}
      <FieldGroup id="scheduling" title="Scheduling">
        <Field label="Mode" help={MODES[text("mode")]} changed={is("mode")}>
          <Select value={text("mode")} onValueChange={set("mode")} disabled={offline}>
            <SelectTrigger aria-label="Mode" className={FIELD_WIDTH}>
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
        <Field
          label="Max concurrent runners"
          htmlFor="global_max"
          help="The most runners running at once across all repositories. Applies in queue mode."
          changed={is("global_max")}
          error={errors.global_max}
        >
          <Input
            id="global_max"
            type="number"
            min={1}
            max={99}
            step={1}
            className={FIELD_WIDTH}
            value={num("global_max")}
            disabled={offline}
            onChange={(e) => set("global_max")(toNum(e.target.value))}
          />
        </Field>
        <Field label="Owner" help="Change it in config.yaml and restart the daemon">
          <span className="font-mono text-sm">{form.config?.owner}</span>
        </Field>
      </FieldGroup>
      <FieldGroup id="timing" title="Timing">
        {unitField("poll_interval", "Poll interval", "How often GitHub is checked. At least 5 seconds.")}
        {unitField("start_timeout", "Start timeout", "A runner not online by then is replaced")}
        {unitField("idle_timeout", "Idle timeout", "Idle runners beyond warm stop after this")}
      </FieldGroup>
      <FieldGroup id="disk" title="Disk and retention">
        <Field label="Disk high-water" htmlFor="disk_high_water" help="Disk use that triggers pruning" changed={is("disk_high_water")} error={errors.disk_high_water}>
          <Input
            id="disk_high_water"
            type="number"
            min={1}
            max={100}
            step={5}
            suffix="%"
            groupClassName={FIELD_WIDTH}
            className="min-w-0"
            value={num("disk_high_water")}
            disabled={offline}
            onChange={(e) => set("disk_high_water")(toNum(e.target.value))}
          />
        </Field>
        {unitField("build_cache_keep", "Build cache keep", "Build cache kept when pruning")}
        {unitField("history_retention", "History retention", "History and logs older than this are removed. At least 1 day.")}
      </FieldGroup>
      <FieldGroup id="runner-defaults" title="Runner defaults" note="Limits apply to newly started runners.">
        <Field label="Global labels" help="Added to every runner" changed={is("labels")}>
          <TagField label="Global labels" value={values.labels as string[]} onChange={set("labels")} disabled={offline} className={FIELD_WIDTH} />
        </Field>
        {unitField("memory_max", "Memory max", "Per runner. Leave it empty for no limit.", "No limit")}
        {unitField("cpu_quota", "CPU quota", "Per runner. 100% is one CPU core.")}
      </FieldGroup>
      {webAccess && (
        <FieldGroup id="web-access" title="Web access" note="Changes apply after a restart.">
          <Field
            label="Listen address"
            htmlFor="web_listen"
            help={form.config?.web?.listen ? undefined : "The web UI turns off at the next restart."}
            changed={is("web_listen")}
            error={errors.web_listen ?? form.rejectedFields.web_listen}
          >
            <Input
              id="web_listen"
              className={`${FIELD_WIDTH} font-mono`}
              value={text("web_listen")}
              disabled={offline}
              onChange={(e) => set("web_listen")(e.target.value)}
            />
          </Field>
          <Field
            label="Allowed hostnames"
            help="IP addresses and localhost always work."
            changed={is("web_hosts")}
            error={errors.web_hosts ?? form.rejectedFields.web_hosts}
          >
            <TagField label="Allowed hostnames" value={values.web_hosts as string[]} onChange={set("web_hosts")} disabled={offline} className={FIELD_WIDTH} />
          </Field>
          {restart?.pending?.length ? (
            <div className="flex flex-wrap items-center gap-x-3 gap-y-2 py-3 text-sm">
              <RestartControl restart={restart} offline={offline} />
            </div>
          ) : null}
        </FieldGroup>
      )}
    </div>
  )
}
