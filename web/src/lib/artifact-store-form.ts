import { useQueryClient } from "@tanstack/react-query"
import { toast } from "darkraise-ui/components/sonner"
import { useState } from "react"
import { ApiError, api } from "@/api/client"
import { keys, readConfig, useConfig, useStatus } from "@/api/hooks"
import type { ArtifactStorePatch, Config } from "@/api/types"
import { changedKeys, settle, type Equal } from "@/lib/draft"
import { errorText } from "@/query"

export type StoreKey = "endpoint" | "bucket" | "region" | "age" | "capacity"
export type StoreValues = Record<StoreKey, string>
type Draft = Partial<StoreValues>

function loadedValues(c: Config): StoreValues {
  const a = c.artifact_store
  return {
    endpoint: a?.endpoint ?? "",
    bucket: a?.bucket ?? "",
    region: a?.region ?? "",
    age: a?.cleanup_max_age ?? "",
    capacity: a?.capacity_gb ? String(a.capacity_gb) : "",
  }
}

const equal: Equal = (_key, a, b) => String(a).trim() === String(b).trim()

function storeErrors(v: StoreValues): Partial<Record<StoreKey, string>> {
  const out: Partial<Record<StoreKey, string>> = {}
  const endpoint = v.endpoint.trim()
  if (endpoint.startsWith("http://")) out.endpoint = "Enter an https:// address; ghr refuses plain http"
  else if (endpoint !== "" && !endpoint.startsWith("https://")) out.endpoint = "Start the address with https://"
  if (endpoint !== "" && v.bucket.trim() === "") out.bucket = "Enter the bucket the endpoint holds"
  return out
}

function toPatch(changed: StoreKey[], v: StoreValues): ArtifactStorePatch {
  const patch: ArtifactStorePatch = {}
  if (changed.includes("endpoint")) patch.endpoint = v.endpoint.trim()
  if (changed.includes("bucket")) patch.bucket = v.bucket.trim()
  if (changed.includes("region")) patch.region = v.region.trim()
  if (changed.includes("age")) patch.cleanup_max_age = v.age.trim()
  if (changed.includes("capacity")) patch.capacity_gb = v.capacity.trim() === "" ? 0 : Number(v.capacity.trim())
  return patch
}

export function useArtifactStoreForm() {
  const config = useConfig()
  const status = useStatus()
  const queryClient = useQueryClient()
  const [draft, setDraft] = useState<Draft>({})
  const [saving, setSaving] = useState(false)
  const [rejection, setRejection] = useState("")

  const loaded = config.data ? loadedValues(config.data) : undefined
  const values: StoreValues = { endpoint: "", bucket: "", region: "", age: "", capacity: "", ...loaded, ...draft }
  const changed = loaded ? (changedKeys(draft, loaded, equal) as StoreKey[]) : []
  const errors = loaded ? storeErrors(values) : {}

  const set = (key: StoreKey) => (value: string) => setDraft((d) => ({ ...d, [key]: value }))

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
    const sent: Draft = Object.fromEntries(changed.map((k) => [k, values[k]]))
    const patch = toPatch(changed, values)
    setSaving(true)
    setRejection("")
    try {
      await api.patchConfig({ artifact_store: patch })
    } catch (err) {
      setSaving(false)
      if (err instanceof ApiError && err.status >= 400 && err.status < 500) {
        setRejection(err.message)
        toast.error("Self-hosted storage not saved")
      } else {
        toast.error(`Self-hosted storage not saved: ${errorText(err)}`)
      }
      return false
    }
    toast.success("Self-hosted storage saved")
    // Settled only after the config is read back, so the fields never flash
    // the values from before the save. When the read fails, the sent values go
    // into the cache instead, or the save would look lost.
    try {
      await readConfig(queryClient)
    } catch (err) {
      toast.error(`Saved, but re-reading the config failed: ${errorText(err)}`)
      queryClient.setQueryData<Config>(keys.config, (c) => c && { ...c, artifact_store: { ...c.artifact_store, ...patch } })
    }
    void queryClient.invalidateQueries({ queryKey: keys.artifactStore })
    setDraft((d) => settle(d, sent, equal) as Draft)
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
    offline: status.isError,
    saving,
    rejection,
    set,
    save,
    discard,
  }
}
