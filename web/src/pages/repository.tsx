import { useQueryClient } from "@tanstack/react-query"
import { Link, useParams } from "@tanstack/react-router"
import { Button } from "darkraise-ui/components/button"
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "darkraise-ui/components/dropdown-menu"
import { toast } from "darkraise-ui/components/sonner"
import { Spinner } from "darkraise-ui/components/spinner"
import { Switch } from "darkraise-ui/components/switch"
import { Tooltip, TooltipContent, TooltipTrigger } from "darkraise-ui/components/tooltip"
import { PageHeader } from "darkraise-ui/layout"
import { Ellipsis } from "lucide-react"
import { useState } from "react"
import { ApiError, api } from "@/api/client"
import { keys, useConfig, useStatus } from "@/api/hooks"
import type { Config, RepoConfig, RepoPatch, RepoStatus } from "@/api/types"
import { ConfirmDialog, type Confirm } from "@/components/confirm-dialog"
import { LabelCheckGroup } from "@/components/label-check-group"
import { NumberField } from "@/components/number-field"
import { ErrorLine } from "@/components/page/error-line"
import { Field, FieldGroup } from "@/components/page/field"
import { SideLayout } from "@/components/page/side-layout"
import { StatusPill } from "@/components/page/status-pill"
import { RegistrationsGroup } from "@/components/registrations-group"
import { RepoActivity } from "@/components/repo-activity"
import { RejectedAlert, SaveBar } from "@/components/save-bar"
import { TagField } from "@/components/tag-field"
import { UnsavedGuard } from "@/components/unsaved-guard"
import { changedKeys, sameValue, settle, type Values } from "@/lib/draft"
import { PUBLIC_HELP, repoDescription, servePublicConfirm } from "@/lib/repos"
import { SIDE_LINK } from "@/lib/side-link"
import { repoStateWord } from "@/lib/status"
import { useNow } from "@/lib/use-now"
import { useRepoActions } from "@/lib/use-repo-actions"
import { errorText } from "@/query"

const SYSTEM_LABELS = ["self-hosted", "linux", "x64"]

const SECTIONS = [
  { id: "", title: "Overview" },
  { id: "settings", title: "Capacity and labels" },
  { id: "labels", title: "Workflow labels" },
  { id: "registrations", title: "GitHub registrations" },
] as const

function repoValues(r: RepoConfig | undefined): Values {
  return {
    max: r?.max,
    warm: r?.warm,
    labels: r?.labels ?? [],
    cleanup_name_prefixes: r?.cleanup_name_prefixes ?? [],
    allow_public: r?.allow_public ?? false,
  }
}


function publicNote(isPublic: boolean | undefined, on: boolean): string {
  if (!isPublic) return ""
  return on ? "GitHub reports this repository public." : "GitHub reports this repository public. Idle runners stop; running jobs finish."
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

function MoreMenu({ name, disabled, onRemove }: { name: string; disabled: boolean; onRemove: () => void }) {
  return (
    <DropdownMenu>
      <Tooltip>
        <TooltipTrigger asChild>
          <DropdownMenuTrigger asChild>
            <Button size="icon" variant="outline" aria-label={`More actions for ${name}`}>
              <Ellipsis size={15} aria-hidden="true" />
            </Button>
          </DropdownMenuTrigger>
        </TooltipTrigger>
        <TooltipContent>More actions</TooltipContent>
      </Tooltip>
      <DropdownMenuContent align="end">
        <DropdownMenuItem disabled={disabled} onSelect={onRemove} className="text-destructive">
          Remove
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

function HistoryLink({ name }: { name: string }) {
  return (
    <Button variant="outline" asChild>
      <Link to="/history" search={{ repo: name }}>
        View history
      </Link>
    </Button>
  )
}

// useRepoActions needs the repository's status, so the live actions mount
// only once it exists.
function LiveActions({ repo, offline }: { repo: RepoStatus; offline: boolean }) {
  const actions = useRepoActions(repo, offline)
  return (
    <>
      <Button variant="outline" disabled={actions.locked} onClick={actions.togglePause}>
        {repo.paused ? "Resume" : "Pause"}
      </Button>
      <HistoryLink name={repo.name} />
      <MoreMenu name={repo.name} disabled={actions.locked} onRemove={actions.askRemove} />
      <ConfirmDialog confirm={actions.confirm} onClose={actions.closeConfirm} />
    </>
  )
}

function HeaderActions({ name, repo, offline }: { name: string; repo: RepoStatus | undefined; offline: boolean }) {
  return (
    <div className="flex flex-wrap gap-2">
      {repo && (
        <span className="flex items-center">
          <StatusPill state={repoStateWord(repo).toLowerCase()} />
        </span>
      )}
      {repo ? (
        <LiveActions repo={repo} offline={offline} />
      ) : (
        <>
          <Button variant="outline" disabled>
            Pause
          </Button>
          <HistoryLink name={name} />
          <MoreMenu name={name} disabled onRemove={() => {}} />
        </>
      )}
    </div>
  )
}

export function RepositoryPage() {
  const { name = "", section = "" } = useParams({ strict: false })
  // Keyed by name: each repository starts with its own empty draft, and
  // moving between its sections keeps the draft.
  return <RepositoryForm key={name} name={name} section={section} />
}

function RepositoryForm({ name, section }: { name: string; section: string }) {
  const status = useStatus()
  const config = useConfig()
  const now = useNow()
  const queryClient = useQueryClient()
  const [draft, setDraft] = useState<Values>({})
  const [saving, setSaving] = useState(false)
  const [rejection, setRejection] = useState("")
  const [question, setQuestion] = useState<{ confirm: Confirm; cancel: () => void } | null>(null)

  const cfg = config.data
  const repoCfg = cfg?.repos?.find((r) => r.name === name)
  const repoStatus = status.data?.repos.find((r) => r.name === name)
  const loaded = repoValues(repoCfg)
  const values: Values = { ...loaded, ...draft }
  const changed = changedKeys(draft, loaded)
  const maxValue = values.max as number | undefined
  const warmValue = (values.warm as number | undefined) ?? 1
  // An emptied Max is undefined here, so warm has no explicit bound, as in the daemon.
  const warmError = maxValue !== undefined && maxValue > 0 && warmValue > maxValue ? "Warm must be <= max" : ""
  const locked = Boolean(repoStatus?.removing) || status.isError

  function setNumber(key: "max" | "warm", n: number) {
    setDraft((d) => ({ ...d, [key]: Number.isNaN(n) ? undefined : Math.min(99, Math.max(0, Math.trunc(n))) }))
  }

  function discard() {
    setDraft({})
    setRejection("")
  }

  // Turning Serve when public on asks first, and the answer covers the whole
  // save: confirming sends every change, Cancel sends nothing and keeps the
  // draft. The promise settles with the answer, so UnsavedGuard waits for it.
  function save(): Promise<boolean> {
    if (warmError) {
      toast.error("Fix the highlighted settings first")
      return Promise.resolve(false)
    }
    if (!changed.includes("allow_public") || draft.allow_public !== true) return send()
    return new Promise((resolve) => {
      setQuestion({
        confirm: servePublicConfirm(name, () => resolve(send())),
        cancel: () => resolve(false),
      })
    })
  }

  function closeQuestion() {
    question?.cancel()
    setQuestion(null)
  }

  async function send(): Promise<boolean> {
    const sent: Values = Object.fromEntries(changed.map((k) => [k, draft[k]]))
    // An emptied Max or Warm box is undefined in the draft; null tells the
    // daemon to clear the value, where a missing key would leave it.
    const patch = Object.fromEntries(changed.map((k) => [k, draft[k] ?? null])) as RepoPatch
    setSaving(true)
    setRejection("")
    try {
      await api.patchConfig({ repos: { [name]: patch } })
    } catch (err) {
      if (err instanceof ApiError && err.status >= 400 && err.status < 500) {
        setRejection(err.message)
        toast.error("Repositories not saved")
      } else {
        toast.error(`Repositories not saved: ${errorText(err)}`)
      }
      setSaving(false)
      return false
    }
    toast.success("Repositories saved")
    try {
      const fresh = await queryClient.fetchQuery({ queryKey: keys.config, queryFn: ({ signal }) => api.config(signal), staleTime: 0 })
      const applied = repoValues(fresh.repos?.find((r) => r.name === name))
      const missed = Object.keys(sent).find((key) => !sameValue(applied[key], sent[key]))
      if (missed) toast.error(`Daemon did not apply ${name}.${missed}; is it older than this ghr?`)
    } catch (err) {
      toast.error(`Saved, but re-reading the config failed: ${errorText(err)}`)
      // Until a read succeeds the cached config shows what was sent; the next
      // poll replaces it with what the daemon holds.
      queryClient.setQueryData<Config>(keys.config, (c) =>
        c && { ...c, repos: c.repos?.map((r) => (r.name === name ? { ...r, ...(sent as Partial<RepoConfig>) } : r)) ?? null },
      )
    }
    setDraft((d) => settle(d, sent))
    setSaving(false)
    return true
  }

  const breadcrumbs = [{ label: "Repositories", href: "/repositories" }, { label: name }]
  if (!cfg) {
    return (
      <div className="flex flex-col gap-4">
        <PageHeader breadcrumbs={breadcrumbs} title={name} />
        {config.isError ? <ErrorLine onRetry={() => void config.refetch()}>{errorText(config.error)}</ErrorLine> : <Spinner label="Loading" />}
      </div>
    )
  }
  if (!repoCfg) {
    return (
      <div className="flex flex-col gap-4">
        <PageHeader breadcrumbs={breadcrumbs} title={name} />
        <div className="flex flex-col items-start gap-2">
          <p className="text-sm text-muted-foreground">Repository not found. It may have been removed.</p>
          <Link to="/repositories" className="text-sm text-primary hover:underline">
            Go to Repositories
          </Link>
        </div>
      </div>
    )
  }

  const custom = customLabels(cfg.labels ?? [], values.labels as string[])
  const effective = [...SYSTEM_LABELS, ...custom.map((c) => c.label)]
  const degraded = status.data?.degraded ?? false
  const is = (key: string) => changed.includes(key)
  const note = publicNote(repoStatus?.public, values.allow_public === true)

  const base = `/repositories/${name}`

  function addLabel(label: string) {
    const current = values.labels as string[]
    if (current.some((l) => l.toLowerCase() === label.toLowerCase())) return
    setDraft((d) => ({ ...d, labels: [...current, label] }))
  }

  return (
    <div className="flex flex-col gap-4">
      <PageHeader
        breadcrumbs={breadcrumbs}
        title={name}
        description={repoStatus ? repoDescription(repoStatus) : undefined}
        actions={<HeaderActions name={name} repo={repoStatus} offline={status.isError} />}
      />
      {repoStatus?.removing && <p className="text-sm text-muted-foreground">Being removed. Running jobs finish first, and settings are read-only.</p>}
      {rejection && <RejectedAlert message={rejection} />}
      <SideLayout
        label="Repository"
        nav={SECTIONS.map((s) =>
          s.id === "" ? (
            <Link key="overview" to="/repositories/$name" params={{ name }} activeOptions={{ exact: true }} className={SIDE_LINK}>
              {s.title}
            </Link>
          ) : (
            <Link key={s.id} to="/repositories/$name/$section" params={{ name, section: s.id }} className={SIDE_LINK}>
              {s.title}
            </Link>
          ),
        )}
      >
        {section === "" && <RepoActivity name={name} repo={repoStatus} now={now} />}
        {section === "settings" && (
          <div className="flex min-w-0 flex-col gap-6">
            <FieldGroup id="capacity" title="Capacity">
              <Field
                label="Max"
                htmlFor="repo-max"
                help="Leave empty for the default: 1 in queue mode, no limit in all mode."
                changed={is("max")}
              >
                <NumberField
                  id="repo-max"
                  label="Max"
                  emptyValue={cfg?.mode === "all" ? 0 : 1}
                  min={0}
                  max={99}
                  precision={0}
                  className="w-28"
                  disabled={locked}
                  value={typeof values.max === "number" ? values.max : Number.NaN}
                  onValueChange={(n) => setNumber("max", n)}
                />
              </Field>
              <Field label="Warm" htmlFor="repo-warm" help="Runners kept ready in all mode. Leave empty for the default of 1." error={warmError} changed={is("warm")}>
                <NumberField
                  id="repo-warm"
                  label="Warm"
                  emptyValue={1}
                  min={0}
                  max={99}
                  precision={0}
                  className="w-28"
                  disabled={locked}
                  value={typeof values.warm === "number" ? values.warm : Number.NaN}
                  onValueChange={(n) => setNumber("warm", n)}
                />
              </Field>
            </FieldGroup>
            <FieldGroup id="visibility" title="Visibility">
              <Field label="Serve when public" htmlFor="repo-allow-public" help={PUBLIC_HELP} changed={is("allow_public")}>
                <Switch
                  id="repo-allow-public"
                  checked={values.allow_public === true}
                  disabled={locked}
                  onCheckedChange={(on) => setDraft((d) => ({ ...d, allow_public: on }))}
                />
                {note && <span className="text-xs text-warning">{note}</span>}
              </Field>
            </FieldGroup>
            <FieldGroup id="labels" title="Labels">
              <Field label="Repo labels" help="Added to this repository's runners." changed={is("labels")}>
                <TagField
                  label="Repo labels"
                  value={values.labels as string[]}
                  disabled={locked}
                  onChange={(next) => setDraft((d) => ({ ...d, labels: next }))}
                />
              </Field>
              <Field label="Runners get" help="Runners already running keep their labels.">
                <ul aria-label="Runner labels" className="flex flex-wrap gap-x-3 gap-y-1 text-sm">
                  {SYSTEM_LABELS.map((l) => (
                    <li key={`system-${l}`} className="font-mono">
                      {l}
                    </li>
                  ))}
                  {custom.map((c) => (
                    <li key={`custom-${c.label}`}>
                      <span className="font-mono">{c.label}</span>
                      {c.global && <span className="ml-1 text-muted-foreground">global</span>}
                    </li>
                  ))}
                </ul>
              </Field>
            </FieldGroup>
            <FieldGroup id="cleanup" title="Cleanup">
              <Field label="Prefixes" help="Container name prefixes removed after each job." changed={is("cleanup_name_prefixes")}>
                <TagField
                  label="Prefixes"
                  value={values.cleanup_name_prefixes as string[]}
                  disabled={locked}
                  onChange={(next) => setDraft((d) => ({ ...d, cleanup_name_prefixes: next }))}
                />
              </Field>
            </FieldGroup>
          </div>
        )}
        {section === "labels" && (
          <LabelCheckGroup name={name} effective={effective} degraded={degraded} degradedReason={status.data?.degraded_reason} disabled={locked} onAddLabel={addLabel} />
        )}
        {section === "registrations" && (
          <RegistrationsGroup name={name} degraded={degraded} degradedReason={status.data?.degraded_reason} disabled={locked} />
        )}
        {!SECTIONS.some((s) => s.id === section) && <p className="text-sm text-muted-foreground">This repository has no such section.</p>}
      </SideLayout>
      <SaveBar count={changed.length} saving={saving} disabled={status.isError} onSave={() => void save()} onDiscard={discard} />
      <UnsavedGuard
        count={changed.length}
        page="Repositories"
        saving={saving}
        allow={(pathname) => pathname === base || pathname.startsWith(`${base}/`)}
        onSave={save}
        onDiscard={discard}
      />
      <ConfirmDialog confirm={question?.confirm ?? null} onClose={closeQuestion} />
    </div>
  )
}
