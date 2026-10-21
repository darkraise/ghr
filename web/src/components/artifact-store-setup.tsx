import { useMutation, useQueryClient } from "@tanstack/react-query"
import { Button } from "darkraise-ui/components/button"
import { Dialog, DialogBody, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "darkraise-ui/components/dialog"
import { Input } from "darkraise-ui/components/input"
import { Label } from "darkraise-ui/components/label"
import { toast } from "darkraise-ui/components/sonner"
import { Check, X } from "lucide-react"
import { useState } from "react"
import { api } from "@/api/client"
import { keys, useArtifactStore, useStatus } from "@/api/hooks"
import type { ArtifactKeyWhich } from "@/api/types"
import { ErrorLine } from "@/components/page/error-line"
import { Field } from "@/components/page/field"
import { Section } from "@/components/page/section"
import { copyText } from "@/lib/clipboard"
import { monthDay } from "@/lib/format"
import { useMounted } from "@/lib/use-mounted"
import { errorText } from "@/query"

export const SETUP_GUIDE = "https://github.com/darkraise/ghr/blob/master/docs/self-hosted-artifacts.md"

const KEYS: Record<ArtifactKeyWhich, { name: string; help: string; about: string }> = {
  admin: {
    name: "Admin key",
    help: "Stays on this host. Lists, downloads and deletes for this page and cleanup. ghr never shows the secret again.",
    about:
      "An access key that can list, read and delete the bucket's objects and abort uploads. ghr keeps it on this host and never shows the secret again.",
  },
  job: {
    name: "Job key",
    help: "ghr hands it to its own runners for the private repositories you turn on and never writes it to GitHub. It can list, read and write the bucket but not delete. A replaced key reaches idle runners at the next check.",
    about:
      "An access key that can list, read and write the bucket but not delete. ghr writes it into the work folder of each runner it starts for a private repository you turn on. All of ghr's runners run as one user, so any job on this host can read it while such a runner is up.",
  },
}

const WHICH: ArtifactKeyWhich[] = ["admin", "job"]

export function KeyFields({ offline }: { offline: boolean }) {
  const store = useArtifactStore()
  const [open, setOpen] = useState<ArtifactKeyWhich | null>(null)
  const info = (which: ArtifactKeyWhich) => (which === "admin" ? store.data?.admin_key : store.data?.job_key)
  return (
    <>
      {WHICH.map((which) => {
        const key = info(which)
        const verb = key ? "Replace" : "Set"
        return (
          <Field key={which} label={KEYS[which].name} help={KEYS[which].help}>
            {key ? (
              <>
                <span className="font-mono text-sm">{key.id}</span>
                <span className="text-sm text-muted-foreground">{`set ${monthDay(new Date(key.set_at))}`}</span>
              </>
            ) : (
              <span className="text-sm text-muted-foreground">{store.data ? "Not set" : "Not read yet"}</span>
            )}
            <Button
              size="sm"
              variant="outline"
              aria-label={`${verb} the ${KEYS[which].name.toLowerCase()}`}
              disabled={offline || !store.data}
              onClick={() => setOpen(which)}
            >
              {verb}
            </Button>
          </Field>
        )
      })}
      <Dialog
        open={open !== null}
        onOpenChange={(o) => {
          if (!o) setOpen(null)
        }}
      >
        {/* Mounted only while open: each opening starts with empty fields, and
            the secret leaves memory with the dialog. */}
        <DialogContent>{open && <KeyForm which={open} replacing={info(open) !== undefined} onClose={() => setOpen(null)} />}</DialogContent>
      </Dialog>
    </>
  )
}

function KeyForm({ which, replacing, onClose }: { which: ArtifactKeyWhich; replacing: boolean; onClose: () => void }) {
  const queryClient = useQueryClient()
  const mounted = useMounted()
  const [id, setId] = useState("")
  const [secret, setSecret] = useState("")
  const [error, setError] = useState("")
  const [busy, setBusy] = useState(false)
  const name = KEYS[which].name

  async function save() {
    if (id.trim() === "" || secret.trim() === "") return setError("Enter both the access key ID and the secret")
    setError("")
    setBusy(true)
    try {
      await api.setArtifactKey(which, id, secret)
      if (mounted.current) onClose()
      toast.success(`${name} saved`)
      await queryClient.invalidateQueries({ queryKey: keys.artifactStore })
    } catch (err) {
      setError(errorText(err))
    } finally {
      setBusy(false)
    }
  }

  return (
    <>
      <DialogHeader>
        <DialogTitle>{`${replacing ? "Replace" : "Set"} the ${name.toLowerCase()}`}</DialogTitle>
        <DialogDescription>{KEYS[which].about}</DialogDescription>
      </DialogHeader>
      <DialogBody className="flex flex-col gap-2">
        <Label htmlFor="artifact-key-id">Access key ID</Label>
        <Input id="artifact-key-id" autoComplete="off" className="font-mono" value={id} onChange={(e) => setId(e.target.value)} />
        <Label htmlFor="artifact-key-secret">Secret access key</Label>
        <Input id="artifact-key-secret" type="password" autoComplete="new-password" value={secret} onChange={(e) => setSecret(e.target.value)} />
        {error && <ErrorLine>{error}</ErrorLine>}
      </DialogBody>
      <DialogFooter>
        <Button variant="outline" onClick={onClose}>
          Cancel
        </Button>
        <Button loading={busy} onClick={() => void save()}>
          Save
        </Button>
      </DialogFooter>
    </>
  )
}

export function ConnectionCheck({ offline }: { offline: boolean }) {
  const test = useMutation({
    mutationFn: () => api.testArtifactStore(),
    onSuccess: (result) => {
      const passed = result.checks.filter((c) => c.ok).length
      const text = `Connection checked: ${passed} of ${result.checks.length} passed`
      if (passed === result.checks.length) toast.success(text)
      else toast.error(text)
    },
  })
  return (
    <Field label="Check" help="Checks use the admin key; a workflow's first upload proves the job key.">
      <Button variant="outline" loading={test.isPending} disabled={offline || test.isPending} onClick={() => test.mutate()}>
        Test connection
      </Button>
      {test.data && (
        <ul aria-label="Connection checks" className="flex w-full flex-col gap-1 text-sm">
          {test.data.checks.map((c) => (
            <li key={c.name} className="flex items-start gap-2">
              {c.ok ? (
                <Check size={15} aria-hidden="true" className="mt-0.5 shrink-0 text-success" />
              ) : (
                <X size={15} aria-hidden="true" className="mt-0.5 shrink-0 text-destructive" />
              )}
              <span>
                <span className="sr-only">{c.ok ? "Passed: " : "Failed: "}</span>
                {c.detail}
              </span>
            </li>
          ))}
        </ul>
      )}
    </Field>
  )
}

const RELEASE = /^v\d+\.\d+\.\d+$/

function workflowSnippet(tag: string): string {
  return [
    `      - uses: darkraise/ghr/actions/upload@${tag}`,
    "        with:",
    "          name: dist",
    "          path: dist/",
    "",
    `      - uses: darkraise/ghr/actions/download@${tag}`,
    "        with:",
    "          name: dist",
  ].join("\n")
}

export function WorkflowsCard() {
  const status = useStatus()
  const installed = status.data?.ghr_update?.installed ?? ""
  const text = workflowSnippet(RELEASE.test(installed) ? installed : "vX.Y.Z")
  async function copy() {
    try {
      await copyText(text)
      toast.success("Workflow snippet copied")
    } catch (err) {
      toast.error(errorText(err))
    }
  }
  return (
    <Section
      id="artifact-workflows"
      title="Workflows"
      aside={
        <Button size="sm" variant="outline" className="ml-auto" onClick={() => void copy()}>
          Copy
        </Button>
      }
    >
      <div className="flex flex-col gap-3">
        <p className="text-sm text-muted-foreground">
          Use ghr's actions in place of actions/upload-artifact and download-artifact. ghr hands the store settings to its own runners, so only jobs on ghr's
          Linux runners for the private repositories you turn on use RustFS. Every other job keeps storing on GitHub, so this is safe to add everywhere, but a
          job on another runner cannot download what a job on ghr's runners stored.
        </p>
        <pre className="overflow-x-auto rounded-md border border-border bg-background p-3 font-mono text-xs">{text}</pre>
        <a href={SETUP_GUIDE} target="_blank" rel="noreferrer" className="text-sm text-primary hover:underline">
          RustFS setup guide: bucket, admin and job key policies
        </a>
      </div>
    </Section>
  )
}
