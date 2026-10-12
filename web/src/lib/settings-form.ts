import { useQueryClient } from "@tanstack/react-query"
import { toast } from "darkraise-ui/components/sonner"
import { useState } from "react"
import { ApiError, api } from "@/api/client"
import { keys, useConfig, useStatus } from "@/api/hooks"
import type { Config, ConfigPatch, RunnerLimitsPatch } from "@/api/types"
import { changedKeys, sameValue, settle, type Equal, type Values } from "@/lib/draft"
import { UNIT_SETTINGS, readSetting, writeSetting } from "@/lib/units"
import { errorText } from "@/query"

// The daemon's cap on global_max (config.MaxRunners); it refuses a larger value.
const MAX_RUNNERS = 99

function loadedValues(c: Config): Values {
  return {
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
  }
}

const equal: Equal = (_key, a, b) => {
  if (typeof a === "number" && typeof b === "number") return Object.is(a, b)
  return typeof a === "string" && typeof b === "string" ? a.trim() === b.trim() : sameValue(a, b)
}

function fieldErrors(v: Values): Record<string, string> {
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
  return errors
}

const isLimit = (key: string): key is keyof RunnerLimitsPatch => key === "memory_max" || key === "cpu_quota"

function toPatch(sent: Values): ConfigPatch {
  const patch: Record<string, unknown> = {}
  const limits: RunnerLimitsPatch = {}
  for (const [key, v] of Object.entries(sent)) {
    const written = writeSetting(key, v)
    const value = typeof written === "string" ? written.trim() : written
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
  set: (key: string) => (value: unknown) => void
  save: () => Promise<boolean>
  discard: () => void
}

export function useSettingsForm(): SettingsForm {
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
      const fresh = loadedValues(
        await queryClient.fetchQuery({ queryKey: keys.config, queryFn: ({ signal }) => api.config(signal), staleTime: 0 }),
      )
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

  return { config: config.data, configError: config.isError ? errorText(config.error) : "", retryConfig: () => void config.refetch(), values, changed, errors, offline, saving, rejection, set, save, discard }
}
