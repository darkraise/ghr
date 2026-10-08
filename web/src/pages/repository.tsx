import { useQueryClient } from "@tanstack/react-query"
import { useParams } from "@tanstack/react-router"
import { Card, CardContent, CardHeader, CardTitle } from "darkraise-ui/components/card"
import { EmptyState } from "darkraise-ui/components/empty-state"
import { Input } from "darkraise-ui/components/input"
import { Label } from "darkraise-ui/components/label"
import { toast } from "darkraise-ui/components/sonner"
import { Spinner } from "darkraise-ui/components/spinner"
import { PageHeader } from "darkraise-ui/layout"
import { useState, type ReactNode } from "react"
import { ApiError, api } from "@/api/client"
import { keys, useConfig, useStatus } from "@/api/hooks"
import type { Config, RepoConfig, RepoPatch } from "@/api/types"
import { ActivitySummary, RepoSummary } from "@/components/repo-summary"
import { LabelCheckGroup } from "@/components/label-check-group"
import { RegistrationsCard } from "@/components/registrations-card"
import { RejectedAlert, SaveBar } from "@/components/save-bar"
import { TagField } from "@/components/tag-field"
import { UnsavedGuard } from "@/components/unsaved-guard"
import { changedKeys, sameValue, settle, type Values } from "@/lib/draft"
import { useNow } from "@/lib/use-now"
import { errorText } from "@/query"

const SYSTEM_LABELS = ["self-hosted", "linux", "x64"]

function repoValues(r: RepoConfig | undefined): Values {
  return { max: r?.max, warm: r?.warm, labels: r?.labels ?? [], cleanup_name_prefixes: r?.cleanup_name_prefixes ?? [] }
}

// Mirrors config.CustomLabels: global then repo labels, trimmed, lower-cased
// and deduplicated, so the preview shows what the runners register.
function customLabels(global: string[], repo: string[]): { label: string; global: boolean }[] {
  const seen = new Set<string>()
  const out: { label: string; global: boolean }[] = []
  for (const [list, isGlobal] of [
    [global, true],
    [repo, false],
  ] as const) {
    for (const raw of list) {
      const label = raw.trim().toLowerCase()
      if (label === "" || seen.has(label)) continue
      seen.add(label)
      out.push({ label, global: isGlobal })
    }
  }
  return out
}

function Row({ label, htmlFor, desc, error, children }: { label: string; htmlFor?: string; desc?: string; error?: string; children: ReactNode }) {
  return (
    <div className="grid gap-1 sm:grid-cols-[10rem_1fr] sm:items-start">
      {htmlFor ? (
        <Label htmlFor={htmlFor} className="pt-2">
          {label}
        </Label>
      ) : (
        <span className="pt-2 text-sm font-medium">{label}</span>
      )}
      <div className="flex flex-col gap-1">
        {children}
        {desc && <p className="text-xs text-muted-foreground">{desc}</p>}
        {error && <p className="text-xs text-destructive">✖ {error}</p>}
      </div>
    </div>
  )
}

export function RepositoryPage() {
  const { name } = useParams({ from: "/app/repositories/$name" })
  // Keyed by name: each repository starts with its own empty draft.
  return <RepositoryForm key={name} name={name} />
}

function RepositoryForm({ name }: { name: string }) {
  const status = useStatus()
  const config = useConfig()
  const now = useNow()
  const queryClient = useQueryClient()
  const [draft, setDraft] = useState<Values>({})
  const [saving, setSaving] = useState(false)
  const [rejection, setRejection] = useState("")

  const cfg = config.data
  const repoCfg = cfg?.repos?.find((r) => r.name === name)
  const repoStatus = status.data?.repos.find((r) => r.name === name)
  const loaded = repoValues(repoCfg)
  const values: Values = { ...loaded, ...draft }
  const changed = changedKeys(draft, loaded)
  const maxValue = values.max as number | undefined
  const warmValue = (values.warm as number | undefined) ?? 1
  // RepoPatch cannot unset a value, so emptying a box that holds one is an
  // error, not a change; the empty edit stays so the box does not refill.
  const blank = (key: "max" | "warm") =>
    key in draft && draft[key] === undefined && loaded[key] !== undefined ? "enter a number from 0 to 99" : ""
  const maxError = blank("max")
  const warmError =
    blank("warm") || (maxValue !== undefined && maxValue > 0 && warmValue > maxValue ? "warm must be <= max" : "")
  const locked = Boolean(repoStatus?.removing) || status.isError

  function setNumber(key: "max" | "warm", text: string) {
    const n = Number(text)
    if (text.trim() !== "" && !Number.isFinite(n)) return
    setDraft((d) => ({ ...d, [key]: text.trim() === "" ? undefined : Math.min(99, Math.max(0, Math.trunc(n))) }))
  }

  function discard() {
    setDraft({})
    setRejection("")
  }

  async function save(): Promise<boolean> {
    if (maxError || warmError) {
      toast.error("fix the highlighted settings first")
      return false
    }
    const sent: Values = Object.fromEntries(changed.map((k) => [k, draft[k]]))
    setSaving(true)
    setRejection("")
    try {
      await api.patchConfig({ repos: { [name]: sent as RepoPatch } })
    } catch (err) {
      if (err instanceof ApiError && err.status >= 400 && err.status < 500) {
        setRejection(err.message)
        toast.error("repositories not saved")
      } else {
        toast.error(`repositories not saved: ${errorText(err)}`)
      }
      setSaving(false)
      return false
    }
    toast.success("Repositories saved")
    try {
      const fresh = await queryClient.fetchQuery({ queryKey: keys.config, queryFn: ({ signal }) => api.config(signal), staleTime: 0 })
      const applied = repoValues(fresh.repos?.find((r) => r.name === name))
      const missed = Object.keys(sent).find((key) => !sameValue(applied[key], sent[key]))
      if (missed) toast.error(`daemon did not apply ${name}.${missed}; is it older than this ghr?`)
    } catch (err) {
      toast.error(`saved, but re-reading the config failed: ${errorText(err)}`)
      // Until a read succeeds the cached config shows what was sent; the next
      // poll replaces it with what the daemon holds.
      queryClient.setQueryData<Config>(keys.config, (c) =>
        c && { ...c, repos: c.repos?.map((r) => (r.name === name ? { ...r, ...(sent as RepoPatch) } : r)) ?? null },
      )
    }
    setDraft((d) => settle(d, sent))
    setSaving(false)
    return true
  }

  const breadcrumbs = [{ label: "Repositories", href: "/repositories" }, { label: name }]
  if (!cfg) {
    return (
      <>
        <PageHeader breadcrumbs={breadcrumbs} title={name} />
        <Spinner label="loading…" />
      </>
    )
  }
  if (!repoCfg) {
    return (
      <>
        <PageHeader breadcrumbs={breadcrumbs} title={name} />
        <EmptyState title="Repository not found" description={`${name} is not in ghr's config; it may have been removed.`} />
      </>
    )
  }

  const custom = customLabels(cfg.labels ?? [], values.labels as string[])
  const effective = [...SYSTEM_LABELS, ...custom.map((c) => c.label)]
  const degraded = status.data?.degraded ?? false

  function addLabel(label: string) {
    const current = values.labels as string[]
    if (current.some((l) => l.toLowerCase() === label.toLowerCase())) return
    setDraft((d) => ({ ...d, labels: [...current, label] }))
  }
  const defaultMax = cfg.mode === "all" ? "∞ (default)" : "1 (default)"

  return (
    <>
      <PageHeader breadcrumbs={breadcrumbs} title={name} />
      {rejection && <RejectedAlert message={rejection} />}
      <div className="grid gap-4 lg:grid-cols-2">
        <Card>
          <CardHeader>
            <CardTitle>Summary</CardTitle>
          </CardHeader>
          <CardContent className="flex flex-col gap-3">
            {repoStatus && <RepoSummary repo={repoStatus} now={now} />}
            <ActivitySummary name={name} retention={cfg.history_retention} now={now} />
          </CardContent>
        </Card>
        <Card>
          <CardHeader>
            <CardTitle>Capacity</CardTitle>
          </CardHeader>
          <CardContent className="flex flex-col gap-4">
            <Row label="Max" htmlFor="repo-max" desc="once set, it stays explicit" error={maxError}>
              <Input
                id="repo-max"
                type="number"
                min={0}
                max={99}
                placeholder={defaultMax}
                disabled={locked}
                value={typeof values.max === "number" ? String(values.max) : ""}
                onChange={(e) => setNumber("max", e.target.value)}
              />
            </Row>
            <Row label="Warm" htmlFor="repo-warm" desc="applies in all mode" error={warmError}>
              <Input
                id="repo-warm"
                type="number"
                min={0}
                max={99}
                placeholder="1 (default)"
                disabled={locked}
                value={typeof values.warm === "number" ? String(values.warm) : ""}
                onChange={(e) => setNumber("warm", e.target.value)}
              />
            </Row>
          </CardContent>
        </Card>
        <Card>
          <CardHeader>
            <CardTitle>Labels</CardTitle>
          </CardHeader>
          <CardContent className="flex flex-col gap-4">
            <Row label="Repo labels" desc="added to this repo's runners">
              <TagField
                label="Repo labels"
                value={values.labels as string[]}
                disabled={locked}
                onChange={(next) => setDraft((d) => ({ ...d, labels: next }))}
              />
            </Row>
            <Row label="Runners get">
              <div className="flex flex-col gap-1 pt-2 text-sm">
                <p>{SYSTEM_LABELS.join(" ")}</p>
                {custom.length > 0 && <p>{custom.map((c) => (c.global ? `${c.label} (global)` : c.label)).join("  ")}</p>}
                <p className="text-muted-foreground">runners already running keep their labels</p>
              </div>
            </Row>
          </CardContent>
        </Card>
        <Card>
          <CardHeader>
            <CardTitle>Cleanup</CardTitle>
          </CardHeader>
          <CardContent>
            <Row label="Prefixes" desc="container name prefixes removed after each job">
              <TagField
                label="Prefixes"
                value={values.cleanup_name_prefixes as string[]}
                disabled={locked}
                onChange={(next) => setDraft((d) => ({ ...d, cleanup_name_prefixes: next }))}
              />
            </Row>
          </CardContent>
        </Card>
      </div>
      <div className="mt-4 grid gap-4 lg:grid-cols-2">
        <LabelCheckGroup name={name} effective={effective} degraded={degraded} degradedReason={status.data?.degraded_reason} disabled={locked} onAddLabel={addLabel} />
        <RegistrationsCard name={name} degraded={degraded} degradedReason={status.data?.degraded_reason} disabled={locked} />
      </div>
      <SaveBar count={changed.length} saving={saving} disabled={status.isError} onSave={() => void save()} onDiscard={discard} />
      <UnsavedGuard count={changed.length} page="Repositories" saving={saving} onSave={save} onDiscard={discard} />
    </>
  )
}
