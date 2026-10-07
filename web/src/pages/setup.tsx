import { useQueryClient } from "@tanstack/react-query"
import { useNavigate } from "@tanstack/react-router"
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "darkraise-ui/components/alert-dialog"
import { Button } from "darkraise-ui/components/button"
import { Input } from "darkraise-ui/components/input"
import { Label } from "darkraise-ui/components/label"
import { Spinner } from "darkraise-ui/components/spinner"
import { Switch } from "darkraise-ui/components/switch"
import { useEffect, useState, type FormEvent, type ReactNode } from "react"
import { api } from "@/api/client"
import { keys, setupStart, useConfig, useSetupState, useStatus, useStorage } from "@/api/hooks"
import { AddRepoDialog } from "@/components/add-repo-dialog"
import { SettingsSections } from "@/components/settings-form"
import { ToolchainsCard } from "@/components/toolchains-card"
import { useSettingsForm } from "@/lib/settings-form"
import { errorText } from "@/query"

const STEPS = ["Password", "GitHub", "Repositories", "Settings", "Toolchains", "Finish"]

interface NavProps {
  onBack?: () => void
  onNext: () => void
  onSkip?: () => void
  nextLabel?: string
  busy?: boolean
}

function Nav({ onBack, onNext, onSkip, nextLabel = "Next", busy }: NavProps) {
  return (
    <div className="mt-6 flex flex-wrap items-center gap-2">
      {onBack && (
        <Button variant="outline" onClick={onBack}>
          Back
        </Button>
      )}
      <Button onClick={onNext} loading={busy}>
        {nextLabel}
      </Button>
      {onSkip && (
        <Button variant="ghost" onClick={onSkip}>
          Skip the rest
        </Button>
      )}
    </div>
  )
}

function GitHubStep({ owner, onSaved }: { owner: string; onSaved: () => void }) {
  const [name, setName] = useState(owner)
  const [token, setToken] = useState("")
  const [error, setError] = useState("")
  const [busy, setBusy] = useState(false)

  async function submit(e: FormEvent) {
    e.preventDefault()
    setError("")
    setBusy(true)
    try {
      // An owner config.yaml already names goes empty: the daemon keeps it.
      await api.setupGitHub(owner ? "" : name.trim(), token)
      onSaved()
    } catch (err) {
      setError(errorText(err))
    } finally {
      setBusy(false)
    }
  }

  return (
    <form className="flex max-w-md flex-col gap-4" onSubmit={(e) => void submit(e)}>
      <div className="flex flex-col gap-2">
        <Label htmlFor="owner">GitHub owner</Label>
        <Input id="owner" value={name} readOnly={owner !== ""} onChange={(e) => setName(e.target.value)} />
      </div>
      <div className="flex flex-col gap-2">
        <Label htmlFor="token">Token</Label>
        <Input id="token" type="password" autoComplete="off" value={token} onChange={(e) => setToken(e.target.value)} />
        <p className="text-xs text-muted-foreground">
          A fine-grained PAT with access to the repositories ghr serves: Administration read/write, Actions read.
        </p>
      </div>
      {error && (
        <p role="alert" className="text-sm text-destructive">
          {error}
        </p>
      )}
      <Button type="submit" loading={busy}>
        Save and start ghr
      </Button>
    </form>
  )
}

function Starting({ timedOut, onRetry }: { timedOut: boolean; onRetry: () => void }) {
  if (!timedOut) {
    return (
      <p className="flex items-center gap-2 text-sm">
        <Spinner />
        <span>Starting ghr…</span>
      </p>
    )
  }
  return (
    <div className="flex flex-col items-start gap-3">
      <p role="alert" className="text-sm text-destructive">
        ghr is still starting; check journalctl -u ghr on the host
      </p>
      <Button variant="outline" onClick={onRetry}>
        Retry
      </Button>
    </div>
  )
}

function RepositoriesStep() {
  const config = useConfig()
  const [adding, setAdding] = useState(false)
  const repos = config.data?.repos ?? []
  return (
    <div className="flex flex-col items-start gap-3">
      {repos.length === 0 ? (
        <p className="text-sm text-muted-foreground">No repositories yet. You can also add them later on the Repositories page.</p>
      ) : (
        <ul className="list-inside list-disc text-sm" aria-label="Configured repositories">
          {repos.map((r) => (
            <li key={r.name}>{r.name}</li>
          ))}
        </ul>
      )}
      <Button variant="outline" onClick={() => setAdding(true)}>
        + Add repository
      </Button>
      <AddRepoDialog open={adding} onClose={() => setAdding(false)} />
    </div>
  )
}

function SettingsStep({ nav }: { nav: NavProps }) {
  const form = useSettingsForm()
  const [asking, setAsking] = useState(false)
  if (!form.config) return <p className="text-sm text-muted-foreground">loading…</p>

  async function saveAndNext() {
    setAsking(false)
    if (await form.save()) nav.onNext()
  }

  function discardAndNext() {
    form.discard()
    setAsking(false)
    nav.onNext()
  }

  return (
    <>
      <SettingsSections form={form} />
      <Button
        variant="outline"
        disabled={form.changed.length === 0 || form.offline}
        loading={form.saving}
        onClick={() => void form.save()}
      >
        Save settings
      </Button>
      <Nav {...nav} onNext={() => (form.changed.length > 0 ? setAsking(true) : nav.onNext())} />
      <AlertDialog
        open={asking}
        onOpenChange={(open) => {
          if (!open) setAsking(false)
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Save your changes?</AlertDialogTitle>
            <AlertDialogDescription>{form.changed.length} setting(s) are not saved yet.</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Stay</AlertDialogCancel>
            <Button variant="outline" onClick={discardAndNext}>
              Discard
            </Button>
            <AlertDialogAction onClick={() => void saveAndNext()}>Save</AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  )
}

function ToolchainsStep({ popular, onPopular }: { popular: boolean; onPopular: (on: boolean) => void }) {
  const status = useStatus()
  const storage = useStorage(status.data)
  return (
    <div className="flex flex-col gap-4">
      <div className="flex items-center gap-3">
        <Switch id="popular" checked={popular} onCheckedChange={onPopular} />
        <Label htmlFor="popular">Popular set</Label>
      </div>
      <p className="text-xs text-muted-foreground">Queues the popular toolchain set when you finish; it installs in the background.</p>
      {storage.data && <ToolchainsCard storage={storage.data} status={status.data} offline={status.isError} />}
    </div>
  )
}

export function SetupPage() {
  const queryClient = useQueryClient()
  const navigate = useNavigate()
  const [startedAt, setStartedAt] = useState<number | null>(null)
  const [timedOut, setTimedOut] = useState(false)
  const state = useSetupState(startedAt !== null)
  const [step, setStep] = useState<number | null>(null)
  const [popular, setPopular] = useState<boolean | null>(null)
  const [finishing, setFinishing] = useState(false)
  const [finishError, setFinishError] = useState("")
  const configured = state.data?.configured ?? false

  useEffect(() => {
    if (startedAt === null) return
    const id = setTimeout(() => setTimedOut(true), setupStart.limitMs)
    return () => clearTimeout(id)
  }, [startedAt])

  useEffect(() => {
    if (startedAt === null || !configured) return
    setStartedAt(null)
    setTimedOut(false)
    setStep(2)
    void queryClient.invalidateQueries({ queryKey: keys.status })
    void queryClient.invalidateQueries({ queryKey: keys.config })
  }, [startedAt, configured, queryClient])

  if (!state.data) {
    if (state.isError) {
      return <p className="p-8 text-center text-sm text-destructive">cannot reach ghr: {errorText(state.error)}</p>
    }
    return (
      <div className="flex min-h-screen items-center justify-center">
        <Spinner />
      </div>
    )
  }
  const s = state.data
  const current = step ?? (s.configured ? 2 : 1)
  const usePopular = popular ?? s.toolchains_pending

  async function finish(toolchains: "popular" | "none") {
    setFinishing(true)
    setFinishError("")
    try {
      await api.setupFinish(toolchains)
      await queryClient.invalidateQueries({ queryKey: keys.setup })
      await queryClient.invalidateQueries({ queryKey: keys.status })
      await navigate({ to: "/" })
    } catch (err) {
      setFinishError(errorText(err))
    } finally {
      setFinishing(false)
    }
  }
  const skip = () => void finish("none")
  const go = (n: number) => () => setStep(n)

  let body: ReactNode
  switch (current) {
    case 1:
      body =
        startedAt !== null || s.starting ? (
          <Starting
            timedOut={timedOut}
            onRetry={() => {
              setTimedOut(false)
              setStartedAt(Date.now())
            }}
          />
        ) : (
          <GitHubStep owner={s.owner} onSaved={() => setStartedAt(Date.now())} />
        )
      break
    case 2:
      body = (
        <>
          <RepositoriesStep />
          <Nav onNext={go(3)} onSkip={skip} />
        </>
      )
      break
    case 3:
      body = <SettingsStep nav={{ onBack: go(2), onNext: go(4), onSkip: skip }} />
      break
    case 4:
      body = (
        <>
          <ToolchainsStep popular={usePopular} onPopular={setPopular} />
          <Nav onBack={go(3)} onNext={go(5)} onSkip={skip} />
        </>
      )
      break
    default:
      body = (
        <>
          <p className="text-sm">ghr is set up.{usePopular ? " Finishing queues the popular toolchain set." : ""}</p>
          <Nav onBack={go(4)} onNext={() => void finish(usePopular ? "popular" : "none")} nextLabel="Finish" busy={finishing} />
        </>
      )
  }

  return (
    <div className="mx-auto max-w-4xl p-4 sm:p-8">
      <h1 className="mb-1 text-2xl font-semibold">Set up ghr</h1>
      <p className="mb-6 text-sm text-muted-foreground">Each step saves as you go; this page stays open to you until you finish.</p>
      <ol className="mb-6 flex flex-wrap gap-x-4 gap-y-1 text-sm" aria-label="Setup steps">
        {STEPS.map((name, i) => {
          const done = i === 0 || (i === 1 && s.configured) || i < current
          return (
            <li
              key={name}
              aria-current={i === current ? "step" : undefined}
              className={i === current ? "font-semibold" : "text-muted-foreground"}
            >
              {done ? "✓" : `${i + 1}.`} {name}
            </li>
          )
        })}
      </ol>
      <section aria-labelledby="setup-step">
        <h2 id="setup-step" className="mb-4 text-lg font-medium">
          {STEPS[current]}
        </h2>
        {body}
        {finishError && (
          <p role="alert" className="mt-3 text-sm text-destructive">
            {finishError}
          </p>
        )}
      </section>
    </div>
  )
}
