import { useMutation, useQueryClient } from "@tanstack/react-query"
import { Button } from "darkraise-ui/components/button"
import { Input } from "darkraise-ui/components/input"
import { Label } from "darkraise-ui/components/label"
import { toast } from "darkraise-ui/components/sonner"
import { Switch } from "darkraise-ui/components/switch"
import { Table, TableBody, TableCell, TableFooter, TableHead, TableHeader, TableRow } from "darkraise-ui/components/table"
import { ChevronRight, Pencil } from "lucide-react"
import { Fragment, useState } from "react"
import { api } from "@/api/client"
import { githubStorageBusy, keys } from "@/api/hooks"
import type { Config, GitHubStorage, RepoStorage } from "@/api/types"
import { ArtifactRepoDetail } from "@/components/artifact-repo-detail"
import { CleanupPreview } from "@/components/cleanup-preview"
import { ConfirmDialog, type Confirm } from "@/components/confirm-dialog"
import { Section } from "@/components/page/section"
import { RetentionAll } from "@/components/retention-all"
import { humanBytes, plural, repositories } from "@/lib/format"
import { CLEANUP_WARNING, RETENTION_WARNING } from "@/lib/quota"
import { errorText } from "@/query"

const NUM = "text-right font-mono whitespace-nowrap"
const WIDE = "hidden sm:table-cell"

type Ask = (c: Confirm) => void

function Sized({ bytes, count, word }: { bytes: number; count: number; word: string }) {
  return (
    <>
      <div>{humanBytes(bytes)}</div>
      <div className="text-xs text-muted-foreground">{plural(count, word)}</div>
    </>
  )
}

function RetentionCell({ repo, offline, ask }: { repo: RepoStorage; offline: boolean; ask: Ask }) {
  const queryClient = useQueryClient()
  const [days, setDays] = useState<string | null>(null)
  const max = repo.retention_max_days || 400
  const set = useMutation({
    mutationFn: (n: number) => api.setRetention(repo.name, n),
    onSuccess: (_d, n) => {
      toast.success(`Retention of ${repo.name} set to ${n} days`)
      setDays(null)
    },
    onSettled: () => queryClient.invalidateQueries({ queryKey: keys.githubStorage }),
  })
  if (days === null) {
    return (
      <span className="flex items-center justify-end gap-1">
        {repo.retention_days > 0 ? `${repo.retention_days} days` : "Unknown"}
        <Button size="icon" variant="ghost" aria-label={`Edit retention of ${repo.name}`} disabled={offline} onClick={() => setDays(String(repo.retention_days || ""))}>
          <Pencil size={14} aria-hidden="true" />
        </Button>
      </span>
    )
  }
  const n = /^\d+$/.test(days) ? Number(days) : Number.NaN
  const ok = n >= 1 && n <= max
  return (
    <div className="flex flex-col items-end gap-1">
      <div className="flex items-center gap-1">
        <Input aria-label={`Retention of ${repo.name}`} type="number" min={1} max={max} suffix="days" groupClassName="w-32" className="min-w-0" value={days} onChange={(e) => setDays(e.target.value)} />
        <Button
          size="sm"
          aria-label={`Save retention of ${repo.name}`}
          disabled={offline || !ok || set.isPending}
          onClick={() =>
            ask({
              title: `Set retention of ${repo.name} to ${n} days?`,
              body: RETENTION_WARNING,
              action: "Set retention",
              run: () => set.mutate(n),
            })
          }
        >
          Save
        </Button>
        <Button size="sm" variant="ghost" onClick={() => setDays(null)}>
          Cancel
        </Button>
      </div>
      {days !== "" && !ok && <p className="text-xs text-destructive">{`Enter a whole number of days from 1 to ${max}`}</p>}
    </div>
  )
}

type Choice = "inherit" | "off" | "age"

function cleanupText(repo: RepoStorage): string {
  return repo.cleanup_source === "none" ? "off" : `${repo.cleanup} (${repo.cleanup_source})`
}

function CleanupCell({ repo, globalAge, offline, ask }: { repo: RepoStorage; globalAge: string; offline: boolean; ask: Ask }) {
  const queryClient = useQueryClient()
  const [choice, setChoice] = useState<Choice | null>(null)
  const [age, setAge] = useState("")
  const save = useMutation({
    mutationFn: (value: string | null) => api.patchConfig({ github_storage: { repos: { [repo.name]: value } } }),
    onSuccess: () => {
      toast.success(`Cleanup of ${repo.name} saved`)
      setChoice(null)
    },
    onSettled: () => {
      void queryClient.invalidateQueries({ queryKey: keys.config })
      void queryClient.invalidateQueries({ queryKey: keys.githubStorage })
    },
  })

  async function submit(c: Choice) {
    const value = c === "inherit" ? null : c === "off" ? "off" : age.trim()
    const effective = c === "inherit" ? globalAge : c === "age" ? age.trim() : ""
    if (effective === "") return save.mutate(value)
    try {
      const previews = await api.storagePreview(effective, repo.name)
      ask({
        title: `Clean ${repo.name}'s artifacts after ${effective}?`,
        body: CLEANUP_WARNING,
        details: <CleanupPreview age={effective} previews={previews} />,
        action: "Save",
        destructive: previews.some((p) => p.count > 0),
        run: () => save.mutate(value),
      })
    } catch (err) {
      toast.error(errorText(err))
    }
  }

  if (choice === null) {
    return (
      <span className="flex items-center gap-1">
        {cleanupText(repo)}
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
        <option value="inherit">{`Global (${globalAge || "off"})`}</option>
        <option value="age">Age</option>
        <option value="off">Off</option>
      </select>
      {choice === "age" && (
        <Input aria-label={`Cleanup age of ${repo.name}`} className="w-20 font-mono" placeholder="6h" value={age} onChange={(e) => setAge(e.target.value)} />
      )}
      <Button size="sm" aria-label={`Save cleanup of ${repo.name}`} disabled={offline || save.isPending || (choice === "age" && age.trim() === "")} onClick={() => void submit(choice)}>
        Save
      </Button>
      <Button size="sm" variant="ghost" onClick={() => setChoice(null)}>
        Cancel
      </Button>
    </div>
  )
}

const isEmpty = (r: RepoStorage) => r.artifact_count === 0 && r.cache_count === 0 && !r.error

const bySize = (a: RepoStorage, b: RepoStorage) =>
  b.artifact_bytes - a.artifact_bytes || b.cache_bytes - a.cache_bytes || a.name.toLowerCase().localeCompare(b.name.toLowerCase())

// A filter with text shows every repository it matches, empty or not; without
// one, empty repositories wait for the switch.
function shownRepos(repos: RepoStorage[], filter: string, showEmpty: boolean): RepoStorage[] {
  const q = filter.trim().toLowerCase()
  return repos.filter((r) => (q ? r.name.toLowerCase().includes(q) : showEmpty || !isEmpty(r))).sort(bySize)
}

export function ArtifactRepoTable({ storage, config, offline }: { storage: GitHubStorage; config: Config | undefined; offline: boolean }) {
  const [confirm, setConfirm] = useState<Confirm | null>(null)
  const [open, setOpen] = useState<string[]>([])
  const [filter, setFilter] = useState("")
  const [showEmpty, setShowEmpty] = useState(false)
  const busy = githubStorageBusy(storage)
  const globalAge = config?.github_storage?.cleanup_max_age ?? ""
  const warned = (storage.quota?.level ?? "") !== ""
  const repos = storage.repos
  const shown = shownRepos(repos, filter, showEmpty)
  const empty = repos.filter(isEmpty).length
  const atDefault = repos.filter((r) => r.retention_days === 90).length
  const sum = (pick: (r: RepoStorage) => number) => repos.reduce((n, r) => n + pick(r), 0)
  return (
    <Section id="repositories" title="Repositories" flush>
      {repos.length === 0 ? (
        <p className="p-4 text-sm text-muted-foreground">No repositories listed yet.</p>
      ) : (
        <>
          <div className="flex flex-col gap-3 border-b border-border p-4">
            <div className="flex flex-wrap items-center gap-x-4 gap-y-2">
              <Input aria-label="Filter repositories" placeholder="Filter repositories" className="w-64 max-w-full" value={filter} onChange={(e) => setFilter(e.target.value)} />
              {empty > 0 && (
                <span className="flex items-center gap-2">
                  <Switch id="show-empty" checked={showEmpty} onCheckedChange={setShowEmpty} />
                  <Label htmlFor="show-empty">{`Show ${empty} empty ${empty === 1 ? "repository" : "repositories"}`}</Label>
                </span>
              )}
            </div>
            {warned && atDefault > 0 && (
              <p className="text-sm text-warning">{`${repositories(atDefault)} ${atDefault === 1 ? "keeps" : "keep"} GitHub's 90-day default; lowering retention saves storage.`}</p>
            )}
            <RetentionAll count={repos.length} offline={offline} busy={busy} ask={setConfirm} />
          </div>
          <div className="overflow-x-auto [&_.dr-table-wrapper]:border-0">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead className="pl-4">Repository</TableHead>
                  <TableHead className="text-right">Artifacts</TableHead>
                  <TableHead className={`text-right ${WIDE}`}>Caches</TableHead>
                  <TableHead className="text-right">Retention</TableHead>
                  <TableHead className="pr-4">Cleanup</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {shown.length === 0 && (
                  <TableRow>
                    <TableCell colSpan={5} className="p-4 text-sm text-muted-foreground">
                      No repository matches.
                    </TableCell>
                  </TableRow>
                )}
                {shown.map((r) => {
                  const expanded = open.includes(r.name)
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
                            {r.visibility === "public" && <span className="text-xs text-muted-foreground">public</span>}
                          </span>
                          {r.error && <p className="text-xs whitespace-normal text-destructive">{r.error}</p>}
                        </th>
                        <TableCell className={`align-top ${NUM}`}>
                          <Sized bytes={r.artifact_bytes} count={r.artifact_count} word="artifact" />
                        </TableCell>
                        <TableCell className={`align-top ${NUM} ${WIDE}`}>
                          <Sized bytes={r.cache_bytes} count={r.cache_count} word="cache" />
                        </TableCell>
                        <TableCell className="text-right align-top">
                          <RetentionCell repo={r} offline={offline} ask={setConfirm} />
                        </TableCell>
                        <TableCell className="pr-4 align-top">
                          <CleanupCell repo={r} globalAge={globalAge} offline={offline || !config} ask={setConfirm} />
                        </TableCell>
                      </TableRow>
                      {expanded && (
                        <TableRow>
                          <TableCell colSpan={5} className="bg-background p-0 whitespace-normal">
                            <ArtifactRepoDetail repo={r} owner={config?.owner ?? ""} busy={busy} offline={offline} ask={setConfirm} />
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
                  <TableCell className={`align-top ${NUM}`}>
                    <Sized bytes={sum((r) => r.artifact_bytes)} count={sum((r) => r.artifact_count)} word="artifact" />
                  </TableCell>
                  <TableCell className={`align-top ${NUM} ${WIDE}`}>
                    <Sized bytes={sum((r) => r.cache_bytes)} count={sum((r) => r.cache_count)} word="cache" />
                  </TableCell>
                  <TableCell colSpan={2} />
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
