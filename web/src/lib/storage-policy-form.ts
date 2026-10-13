import { useQueryClient } from "@tanstack/react-query"
import { toast } from "darkraise-ui/components/sonner"
import { useState } from "react"
import { ApiError, api } from "@/api/client"
import { keys, useConfig, useStatus } from "@/api/hooks"
import type { Config, GitHubStoragePatch, StoragePreview } from "@/api/types"
import { changedKeys, settle, type Equal } from "@/lib/draft"
import { errorText } from "@/query"

export type PolicyKey = "included" | "warn" | "critical" | "age"
export type PolicyValues = Record<PolicyKey, string>
type Draft = Partial<PolicyValues>

// A changed, non-empty cleanup age waits here until its preview is confirmed.
export interface PendingCleanup {
  age: string
  previews: StoragePreview[]
  sent: Draft
  patch: GitHubStoragePatch
}

function loadedValues(c: Config): PolicyValues {
  const g = c.github_storage
  const [warn, critical] = g?.warn_pct ?? [70, 90]
  return { included: String(g?.included_mb ?? 500), warn: String(warn), critical: String(critical), age: g?.cleanup_max_age ?? "" }
}

const equal: Equal = (_key, a, b) => String(a).trim() === String(b).trim()

const whole = (s: string) => (/^\d+$/.test(s.trim()) ? Number(s.trim()) : Number.NaN)

function policyErrors(v: PolicyValues): Partial<Record<PolicyKey, string>> {
  const out: Partial<Record<PolicyKey, string>> = {}
  if (!(whole(v.included) >= 1)) out.included = "Enter a whole number of MB, at least 1"
  const w = whole(v.warn)
  const c = whole(v.critical)
  if (!(w >= 1 && c <= 100 && w < c)) out.critical = "Enter two whole percentages from 1 to 100, the first lower"
  return out
}

function toPatch(changed: PolicyKey[], v: PolicyValues): GitHubStoragePatch {
  const patch: GitHubStoragePatch = {}
  if (changed.includes("included")) patch.included_mb = whole(v.included)
  if (changed.includes("warn") || changed.includes("critical")) patch.warn_pct = [whole(v.warn), whole(v.critical)]
  if (changed.includes("age")) patch.cleanup_max_age = v.age.trim()
  return patch
}

export function useStoragePolicyForm() {
  const config = useConfig()
  const status = useStatus()
  const queryClient = useQueryClient()
  const [draft, setDraft] = useState<Draft>({})
  const [saving, setSaving] = useState(false)
  const [rejection, setRejection] = useState("")
  const [pending, setPending] = useState<PendingCleanup | null>(null)

  const loaded = config.data ? loadedValues(config.data) : undefined
  const values: PolicyValues = { included: "", warn: "", critical: "", age: "", ...loaded, ...draft }
  const changed = loaded ? (changedKeys(draft, loaded, equal) as PolicyKey[]) : []
  const errors = loaded ? policyErrors(values) : {}

  const set = (key: PolicyKey) => (value: string) => setDraft((d) => ({ ...d, [key]: value }))

  function discard() {
    setDraft({})
    setRejection("")
  }

  async function send(sent: Draft, patch: GitHubStoragePatch): Promise<boolean> {
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
      return false
    }
    toast.success("Storage policy saved")
    // Settled only after the config is read back, so the fields never flash
    // the values from before the save.
    await queryClient.invalidateQueries({ queryKey: keys.config })
    void queryClient.invalidateQueries({ queryKey: keys.githubStorage })
    setDraft((d) => settle(d, sent, equal) as Draft)
    setSaving(false)
    return true
  }

  async function save(): Promise<boolean> {
    if (changed.length === 0) return true
    if (saving || pending) return false
    if (Object.keys(errors).length > 0) {
      toast.error("Fix the highlighted settings first")
      return false
    }
    const sent: Draft = Object.fromEntries(changed.map((k) => [k, values[k]]))
    const patch = toPatch(changed, values)
    const age = patch.cleanup_max_age
    if (age === undefined || age === "") return send(sent, patch)
    setRejection("")
    try {
      setPending({ age, previews: await api.storagePreview(age), sent, patch })
    } catch (err) {
      setRejection(errorText(err))
      toast.error("Storage policy not saved")
    }
    // The preview's confirmation sends the save, so a leave prompt that asked
    // for this one keeps the page.
    return false
  }

  function confirmPending() {
    if (!pending) return
    const p = pending
    setPending(null)
    void send(p.sent, p.patch)
  }

  return {
    config: config.data,
    configError: config.isError ? errorText(config.error) : "",
    retryConfig: () => void config.refetch(),
    values,
    changed,
    errors,
    offline: status.isError,
    saving,
    rejection,
    pending,
    set,
    save,
    discard,
    confirmPending,
    cancelPending: () => setPending(null),
  }
}
