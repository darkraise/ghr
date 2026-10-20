import { useMutation, useQueryClient } from "@tanstack/react-query"
import { Button } from "darkraise-ui/components/button"
import { Input } from "darkraise-ui/components/input"
import { toast } from "darkraise-ui/components/sonner"
import { Switch } from "darkraise-ui/components/switch"
import { Table, TableBody, TableCell, TableFooter, TableHead, TableHeader, TableRow } from "darkraise-ui/components/table"
import { ChevronRight, Pencil } from "lucide-react"
import { Fragment, useState } from "react"
import { api } from "@/api/client"
import { artifactStoreBusy, keys } from "@/api/hooks"
import type { ArtifactStore, ArtifactStoreRepo, Config } from "@/api/types"
import { ArtifactStoreItems } from "@/components/artifact-store-items"
import { ConfirmDialog, type Confirm } from "@/components/confirm-dialog"
import { Section } from "@/components/page/section"
import { globalCleanup, olderThan, STATE_TONE, stateLine } from "@/lib/artifact-store"
import { humanBytes, plural, repositories } from "@/lib/format"
import { useNow } from "@/lib/use-now"

const NUM = "text-right font-mono whitespace-nowrap"

type Ask = (c: Confirm) => void

function Sized({ bytes, count }: { bytes: number; count: number }) {
  return (
    <>
      <div>{humanBytes(bytes)}</div>
      <div className="text-xs text-muted-foreground">{plural(count, "artifact")}</div>
    </>
  )
}

const unset = (r: ArtifactStoreRepo) => r.state === "error" || r.state === "foreign"

function RepoSwitch({ repo, offline }: { repo: ArtifactStoreRepo; offline: boolean }) {
  const queryClient = useQueryClient()
  const set = useMutation({
    mutationFn: (enabled: boolean) => api.setArtifactRepo(repo.name, { enabled }),
    onSuccess: (row, enabled) => {
      if (unset(row)) toast.error(`${repo.name}: ${stateLine(row).text}`)
      else if (!enabled) toast.success(`Turned off for ${repo.name}: variable and secret removed; its artifacts stay until they expire`)
      else if (row.state === "set") toast.success(`Turned on for ${repo.name}: variable and secret set`)
      else toast.success(`Turned on for ${repo.name}; the next check writes its variable and secret`)
    },
    onSettled: () => queryClient.invalidateQueries({ queryKey: keys.artifactStore }),
  })
  const locked = !repo.private
  const on = set.isPending ? (set.variables ?? repo.enabled) : repo.enabled
  return (
    <span className="flex items-center gap-2">
      <Switch
        checked={on}
        disabled={offline || locked || set.isPending}
        aria-label={`Use RustFS for ${repo.name}`}
        onCheckedChange={(next) => set.mutate(next)}
      />
      <span className={`text-sm ${locked ? "text-muted-foreground" : ""}`}>{on ? "On" : "Off"}</span>
    </span>
  )
}

type Choice = "inherit" | "off" | "age"

function CleanupCell({ repo, globalAge, offline, ask }: { repo: ArtifactStoreRepo; globalAge: string; offline: boolean; ask: Ask }) {
  const queryClient = useQueryClient()
  const now = useNow()
  const [choice, setChoice] = useState<Choice | null>(null)
  const [age, setAge] = useState("")
  const save = useMutation({
    mutationFn: (cleanup: string) => api.setArtifactRepo(repo.name, { cleanup }),
    onSuccess: () => {
      toast.success(`Cleanup of ${repo.name} saved`)
      setChoice(null)
    },
    onSettled: () => queryClient.invalidateQueries({ queryKey: keys.artifactStore }),
  })

  function submit(c: Choice) {
    const value = c === "inherit" ? "" : c === "off" ? "off" : age.trim()
    const effective = c === "inherit" ? globalAge : value
    const unexpiring = repo.items.filter((i) => !i.expires_at)
    const old = effective === "off" ? [] : (olderThan(unexpiring, effective, now) ?? [])
    if (old.length === 0) return save.mutate(value)
    const bytes = old.reduce((n, i) => n + i.bytes, 0)
    ask({
      title: `Clean ${repo.name}'s artifacts after ${effective}?`,
      body: `${plural(old.length, "artifact")} (${humanBytes(bytes)}) older than ${effective} would go at the next check, unless their run is still going. Artifacts uploaded with retention-days keep their own expiry. A run that downloads them later fails.`,
      action: "Save",
      destructive: true,
      run: () => save.mutate(value),
    })
  }

  // The daemon refuses a public repository's settings.
  if (!repo.private) return <span className="text-muted-foreground">-</span>
  if (choice === null) {
    return (
      <span className="flex items-center gap-1">
        {`${repo.cleanup} (${repo.cleanup_source})`}
        <Button
          size="icon"
          variant="ghost"
          aria-label={`Edit cleanup of ${repo.name}`}
          disabled={offline}
          onClick={() => {
            setChoice(repo.cleanup_source === "override" ? (repo.cleanup === "off" ? "off" : "age") : "inherit")
            setAge(repo.cleanup_source === "override" && repo.cleanup !== "off" ? repo.cleanup : "")
          }}
        >
          <Pencil size={14} aria-hidden="true" />
        </Button>
      </span>
    )
  }
  return (
    <div className="flex flex-wrap items-center gap-1">
      <select
        aria-label={`Cleanup of ${repo.name}`}
        className="h-8 rounded-md border border-input bg-background px-2 text-sm"
        value={choice}
        onChange={(e) => setChoice(e.target.value as Choice)}
      >
        <option value="inherit">{`Global (${globalAge})`}</option>
        <option value="age">Age</option>
        <option value="off">Off</option>
      </select>
      {choice === "age" && (
        <Input aria-label={`Cleanup age of ${repo.name}`} className="w-20 font-mono" placeholder="3d" value={age} onChange={(e) => setAge(e.target.value)} />
      )}
      <Button
        size="sm"
        aria-label={`Save cleanup of ${repo.name}`}
        disabled={offline || save.isPending || (choice === "age" && age.trim() === "")}
        onClick={() => submit(choice)}
      >
        Save
      </Button>
      <Button size="sm" variant="ghost" onClick={() => setChoice(null)}>
        Cancel
      </Button>
    </div>
  )
}

const bySize = (a: ArtifactStoreRepo, b: ArtifactStoreRepo) => b.bytes - a.bytes || a.name.toLowerCase().localeCompare(b.name.toLowerCase())

export function ArtifactStoreTable({ store, config, offline }: { store: ArtifactStore; config: Config | undefined; offline: boolean }) {
  const queryClient = useQueryClient()
  const [confirm, setConfirm] = useState<Confirm | null>(null)
  const [open, setOpen] = useState<string[]>([])
  const [filter, setFilter] = useState("")
  const busy = artifactStoreBusy(store)
  const globalAge = globalCleanup(config)
  const repos = store.repos
  const q = filter.trim().toLowerCase()
  const shown = repos.filter((r) => r.name.toLowerCase().includes(q)).sort(bySize)
  // Another ghr's variable is never changed, so its repository is not offered.
  const waiting = repos.filter((r) => r.private && !r.enabled && r.state !== "foreign")
  const sum = (pick: (r: ArtifactStoreRepo) => number) => repos.reduce((n, r) => n + pick(r), 0)

  const allOn = useMutation({
    mutationFn: () => api.enablePrivateArtifactRepos(),
    onSuccess: (rows) => {
      const failed = rows.filter(unset)
      if (failed.length === 0) toast.success("Turned on for every private repository")
      else toast.error(`Could not turn on ${failed.map((r) => r.name).join(", ")}; their rows say why`)
    },
    onSettled: () => queryClient.invalidateQueries({ queryKey: keys.artifactStore }),
  })

  function turnAllOn() {
    if (waiting.length === 0) {
      toast("Every private repository is already on")
      return
    }
    setConfirm({
      title: `Turn on RustFS for ${repositories(waiting.length)}?`,
      body: `ghr sets the GHR_ARTIFACT_STORE variable and GHR_ARTIFACT_KEY secret of ${waiting.map((r) => r.name).join(", ")}. Their workflows then upload to RustFS.`,
      action: "Turn on",
      run: () => allOn.mutate(),
    })
  }

  return (
    <Section id="rustfs-repositories" title="Repositories" flush>
      {repos.length === 0 ? (
        <p className="p-4 text-sm text-muted-foreground">No repositories yet; add or watch them on the Repositories page.</p>
      ) : (
        <>
          <div className="flex flex-col gap-3 border-b border-border p-4">
            <div className="flex flex-wrap items-center gap-x-4 gap-y-2">
              <Input aria-label="Filter repositories" placeholder="Filter repositories" className="w-64 max-w-full" value={filter} onChange={(e) => setFilter(e.target.value)} />
              <Button variant="outline" disabled={offline || allOn.isPending} onClick={turnAllOn}>
                Turn on for all private repositories
              </Button>
            </div>
            <p className="text-xs text-muted-foreground">
              Turning a repository on sets its GHR_ARTIFACT_STORE variable and GHR_ARTIFACT_KEY secret; its workflows then upload to RustFS.
            </p>
          </div>
          <div className="overflow-x-auto [&_.dr-table-wrapper]:border-0">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead className="pl-4">Repository</TableHead>
                  <TableHead>Use RustFS</TableHead>
                  <TableHead className="text-right">Artifacts</TableHead>
                  <TableHead className="pr-4">Cleanup</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {shown.length === 0 && (
                  <TableRow>
                    <TableCell colSpan={4} className="p-4 text-sm text-muted-foreground">
                      No repository matches.
                    </TableCell>
                  </TableRow>
                )}
                {shown.map((r) => {
                  const expanded = open.includes(r.name)
                  const line = stateLine(r)
                  return (
                    <Fragment key={r.name}>
                      <TableRow>
                        <th scope="row" className="py-2 pl-4 text-left align-top font-normal">
                          <span className="flex items-center gap-2">
                            <button
                              type="button"
                              aria-expanded={expanded}
                              className="flex items-center gap-1 hover:text-primary"
                              onClick={() => setOpen((o) => (expanded ? o.filter((n) => n !== r.name) : [...o, r.name]))}
                            >
                              <ChevronRight size={14} aria-hidden="true" className={expanded ? "rotate-90" : ""} />
                              {r.name}
                            </button>{" "}
                            {!r.private && <span className="text-xs text-muted-foreground">public</span>}
                          </span>
                          <p className={`text-xs whitespace-normal ${STATE_TONE[line.tone]}`}>{line.text}</p>
                        </th>
                        <TableCell className="align-top">
                          <RepoSwitch repo={r} offline={offline} />
                        </TableCell>
                        <TableCell className={`align-top ${NUM}`}>
                          <Sized bytes={r.bytes} count={r.count} />
                        </TableCell>
                        <TableCell className="pr-4 align-top">
                          <CleanupCell repo={r} globalAge={globalAge} offline={offline} ask={setConfirm} />
                        </TableCell>
                      </TableRow>
                      {expanded && (
                        <TableRow>
                          <TableCell colSpan={4} className="bg-background p-0 whitespace-normal">
                            <ArtifactStoreItems repo={r} offline={offline} busy={busy} ask={setConfirm} />
                          </TableCell>
                        </TableRow>
                      )}
                    </Fragment>
                  )
                })}
              </TableBody>
              <TableFooter>
                <TableRow>
                  <th scope="row" className="py-2 pl-4 text-left align-top font-semibold">
                    {`All ${repositories(repos.length)}`}
                  </th>
                  <TableCell />
                  <TableCell className={`align-top ${NUM}`}>
                    <Sized bytes={sum((r) => r.bytes)} count={sum((r) => r.count)} />
                  </TableCell>
                  <TableCell />
                </TableRow>
              </TableFooter>
            </Table>
          </div>
        </>
      )}
      <ConfirmDialog confirm={confirm} onClose={() => setConfirm(null)} />
    </Section>
  )
}
