import { useQueryClient } from "@tanstack/react-query"
import { toast } from "darkraise-ui/components/sonner"
import { useState } from "react"
import { ApiError, api } from "@/api/client"
import { keys, readConfig, useConfig, useStatus } from "@/api/hooks"
import type { Config, ConfigPatch, JobName, JobsConfig, RunnerLimitsPatch, WebPatch } from "@/api/types"
import { changedKeys, prune, sameValue, settle, type Equal, type Values } from "@/lib/draft"
import { isHostname, splitListen } from "@/lib/listen"
import { UNIT_SETTINGS, readSetting, writeSetting } from "@/lib/units"
import { errorText } from "@/query"

// The daemon's cap on global_max (config.MaxRunners); it refuses a larger value.
const MAX_RUNNERS = 99

export const JOB_NAMES: readonly JobName[] = ["ghr_update_check", "runner_update_check", "prune", "storage_measure"]

// The keys Settings › General and the setup wizard edit; the Jobs page edits
// JOB_KEYS. Each form counts and saves only its own.
export const GENERAL_KEYS: readonly string[] = [
  "mode",
  "global_max",
  "poll_interval",
  "start_timeout",
  "idle_timeout",
  "disk_high_water",
  "build_cache_keep",
  "history_retention",
  "labels",
  "memory_max",
  "cpu_quota",
  "web_listen",
  "web_hosts",
]

export const JOB_KEYS: readonly string[] = JOB_NAMES.flatMap((n) => [`jobs.${n}.enabled`, `jobs.${n}.every`])

const JOB_KEY = /^jobs\.([a-z_]+)\.(enabled|every)$/

function loadedValues(c: Config): Values {
  const values: Values = {
    mode: c.mode,
    global_max: c.global_max,
    poll_interval: readSetting("poll_interval", c.poll_interval),
    start_timeout: readSetting("start_timeout", c.start_timeout),
    idle_timeout: readSetting("idle_timeout", c.idle_timeout),
    disk_high_water: c.disk_high_water,
    build_cache_keep: readSetting("build_cache_keep", c.build_cache_keep),
    history_retention: readSetting("history_retention", c.history_retention),
    labels: c.labels ?? [],
    memory_max: readSetting("memory_max", c.runner_limits.memory_max),
    cpu_quota: readSetting("cpu_quota", c.runner_limits.cpu_quota),
    web_listen: c.web?.listen ?? "",
    web_hosts: c.web?.hosts ?? [],
  }
  // An older daemon sends no jobs block; the Jobs page then has no form.
  for (const name of JOB_NAMES) {
    const job = c.jobs?.[name]
    if (!job) continue
    values[`jobs.${name}.enabled`] = job.enabled
    values[`jobs.${name}.every`] = readSetting(`jobs.${name}.every`, job.every)
  }
  return values
}

// The daemon compares hostnames as a set, ignoring case, order and repeats.
function hostSet(hosts: unknown[]): Set<string> {
  return new Set(hosts.map((h) => String(h).toLowerCase()))
}

const equal: Equal = (key, a, b) => {
  if (key === "web_hosts" && Array.isArray(a) && Array.isArray(b)) {
    const x = hostSet(a)
    const y = hostSet(b)
    return x.size === y.size && [...x].every((h) => y.has(h))
  }
  if (typeof a === "number" && typeof b === "number") return Object.is(a, b)
  return typeof a === "string" && typeof b === "string" ? a.trim() === b.trim() : sameValue(a, b)
}

function fieldErrors(v: Values, loaded: Values): Record<string, string> {
  const errors: Record<string, string> = {}
  for (const [key, spec] of Object.entries(UNIT_SETTINGS)) {
    const value = v[key]
    const err = typeof value === "number" ? spec.error(value) : ""
    if (err) errors[key] = err
  }
  const max = Number(v.global_max)
  if (!Number.isInteger(max) || max < 1 || max > MAX_RUNNERS) errors.global_max = `Enter a whole number from 1 to ${MAX_RUNNERS}`
  const high = Number(v.disk_high_water)
  if (!Number.isInteger(high) || high < 1 || high > 100) errors.disk_high_water = "Enter a whole number from 1 to 100"
  // Only an edit is checked: a saved value is the daemon's, and a hand edit
  // this check would flag must not block saving other fields. An emptied
  // address is left to the daemon, which refuses it with its own reason.
  const listen = String(v.web_listen ?? "").trim()
  if (!equal("web_listen", v.web_listen, loaded.web_listen) && listen !== "" && !splitListen(listen)) {
    errors.web_listen = "Enter host:port with a port from 1 to 65535, such as 0.0.0.0:8080"
  }
  if (!equal("web_hosts", v.web_hosts, loaded.web_hosts)) {
    const bad = ((v.web_hosts as string[] | undefined) ?? []).find((h) => !isHostname(h))
    if (bad !== undefined) errors.web_hosts = `${bad} is not a bare hostname such as ghr.lan`
  }
  return errors
}

const isLimit = (key: string): key is keyof RunnerLimitsPatch => key === "memory_max" || key === "cpu_quota"

function toPatch(sent: Values): ConfigPatch {
  const patch: Record<string, unknown> = {}
  const limits: RunnerLimitsPatch = {}
  const web: WebPatch = {}
  const jobs: Record<string, Record<string, unknown>> = {}
  for (const [key, v] of Object.entries(sent)) {
    const written = writeSetting(key, v)
    const value = typeof written === "string" ? written.trim() : written
    const job = JOB_KEY.exec(key)
    if (job) {
      const [, name = "", field = ""] = job
      jobs[name] = { ...jobs[name], [field]: value }
    } else if (isLimit(key)) limits[key] = value as string
    else if (key === "web_listen") web.listen = value as string
    else if (key === "web_hosts") web.hosts = value as string[]
    else patch[key] = value
  }
  if (Object.keys(limits).length > 0) patch.runner_limits = limits
  if (Object.keys(web).length > 0) patch.web = web
  if (Object.keys(jobs).length > 0) patch.jobs = jobs
  return patch as ConfigPatch
}

function mergeJobs(jobs: JobsConfig | undefined, patch: ConfigPatch["jobs"]): JobsConfig | undefined {
  if (!jobs || !patch) return jobs
  const out = { ...jobs }
  for (const name of JOB_NAMES) out[name] = { ...out[name], ...patch[name] }
  return out
}

// The daemon took the save, so until a read succeeds the cached config shows
// what was sent; the next poll replaces it with what the daemon holds.
function withPatch(c: Config, patch: ConfigPatch): Config {
  const { runner_limits: limits, web, jobs, ...rest } = patch
  return {
    ...c,
    ...rest,
    runner_limits: { ...c.runner_limits, ...limits },
    web: { ...c.web, ...web },
    jobs: mergeJobs(c.jobs, jobs),
  } as Config
}

const PATCH_KEYS: Record<string, string> = {
  memory_max: "runner_limits.memory_max",
  cpu_quota: "runner_limits.cpu_quota",
  web_listen: "web.listen",
  web_hosts: "web.hosts",
}

const patchKey = (key: string) => PATCH_KEYS[key] ?? key

// The daemon starts a web setting's refusal with its key, so the form shows
// it under that field. The rest of the message is one reason, which may hold
// "; " itself, so it is not split like the alert's list.
const REJECTION_FIELDS: [prefix: string, key: string][] = [
  ["web.listen: ", "web_listen"],
  ["web.hosts: ", "web_hosts"],
  ...JOB_NAMES.map((n): [string, string] => [`jobs.${n}.every: `, `jobs.${n}.every`]),
]

function placeRejection(message: string): { fields: Record<string, string>; rest: string } {
  const hit = REJECTION_FIELDS.find(([prefix]) => message.startsWith(prefix))
  return hit ? { fields: { [hit[1]]: message.slice(hit[0].length) }, rest: "" } : { fields: {}, rest: message }
}

export interface SettingsForm {
  config: Config | undefined
  configError: string
  retryConfig: () => void
  values: Values
  changed: string[]
  errors: Record<string, string>
  offline: boolean
  saving: boolean
  rejection: string
  // A rejection placed under its field, by draft key.
  rejectedFields: Record<string, string>
  set: (key: string) => (value: unknown) => void
  save: () => Promise<boolean>
  discard: () => void
}

export function useSettingsForm(fields: readonly string[] = GENERAL_KEYS): SettingsForm {
  const config = useConfig()
  const status = useStatus()
  const queryClient = useQueryClient()
  const [draft, setDraft] = useState<Values>({})
  const [saving, setSaving] = useState(false)
  const [rejection, setRejection] = useState("")

  const loaded = config.data ? loadedValues(config.data) : {}
  // During a save the loaded values are the ones being replaced, so an edit
  // back to one of them is a real edit; settle decides about it afterwards.
  const pruned = config.data && !saving ? prune(draft, loaded) : draft
  if (pruned !== draft) setDraft(pruned)
  const values: Values = { ...loaded, ...draft }
  const changed = config.data ? changedKeys(draft, loaded, equal).filter((k) => fields.includes(k)) : []
  const errors = config.data ? Object.fromEntries(Object.entries(fieldErrors(values, loaded)).filter(([k]) => fields.includes(k))) : {}
  const offline = status.isError
  const placed = placeRejection(rejection)

  const set = (key: string) => (value: unknown) => setDraft((d) => ({ ...d, [key]: value }))

  function discard() {
    setDraft({})
    setRejection("")
  }

  async function save(): Promise<boolean> {
    if (changed.length === 0) return true
    if (saving) return false
    if (Object.keys(errors).length > 0) {
      toast.error("Fix the highlighted settings first")
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
        toast.error("Settings not saved")
      } else {
        toast.error(`Settings not saved: ${errorText(err)}`)
      }
      return false
    }
    toast.success("Settings saved")
    try {
      const fresh = loadedValues(await readConfig(queryClient))
      const missed = Object.keys(sent).find((key) => !equal(key, fresh[key], sent[key]))
      if (missed) toast.error(`Daemon did not apply ${patchKey(missed)}; is it older than this ghr?`)
    } catch (err) {
      toast.error(`Saved, but re-reading the config failed: ${errorText(err)}`)
      queryClient.setQueryData<Config>(keys.config, (c) => c && withPatch(c, toPatch(sent)))
    }
    setDraft((d) => settle(d, sent, equal))
    setSaving(false)
    return true
  }

  return {
    config: config.data,
    configError: config.isError ? errorText(config.error) : "",
    retryConfig: () => void config.refetch(),
    values,
    changed,
    errors,
    offline,
    saving,
    rejection: placed.rest,
    rejectedFields: placed.fields,
    set,
    save,
    discard,
  }
}
