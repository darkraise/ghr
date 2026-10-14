import { useMutation, useQueryClient } from "@tanstack/react-query"
import { Link } from "@tanstack/react-router"
import { Button } from "darkraise-ui/components/button"
import { Input } from "darkraise-ui/components/input"
import { toast } from "darkraise-ui/components/sonner"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "darkraise-ui/components/table"
import { Trash2 } from "lucide-react"
import { useState, type ReactNode } from "react"
import { api } from "@/api/client"
import { keys } from "@/api/hooks"
import type { ArtifactItem, RepoStorage } from "@/api/types"
import type { Confirm } from "@/components/confirm-dialog"
import { ago, dateTime, humanBytes, plural } from "@/lib/format"
import { useNow } from "@/lib/use-now"
import { errorText } from "@/query"

const NUM = "text-right font-mono whitespace-nowrap"
const WIDE = "hidden sm:table-cell"

function Part({ title, label, actions, children }: { title: string; label: string; actions: ReactNode; children: ReactNode }) {
  return (
    <section aria-label={label} className="flex min-w-0 flex-col gap-2">
      <div className="flex flex-wrap items-center gap-2">
        <h3 className="text-sm font-semibold">{title}</h3>
        <div className="ml-auto flex flex-wrap items-center gap-2">{actions}</div>
      </div>
      {children}
    </section>
  )
}

function RunLink({ repo, owner, artifact }: { repo: RepoStorage; owner: string; artifact: ArtifactItem }) {
  if (artifact.run_id === 0) return <>Unknown</>
  const text = `Run ${artifact.run_id}`
  if (repo.covered) {
    return (
      <Link to="/actions/$repo/runs/$id" params={{ repo: repo.name, id: String(artifact.run_id) }} className="hover:text-primary hover:underline">
        {text}
      </Link>
    )
  }
  return (
    <a href={`https://github.com/${owner}/${repo.name}/actions/runs/${artifact.run_id}`} target="_blank" rel="noreferrer" className="hover:text-primary hover:underline">
      {text}
    </a>
  )
}

export function ArtifactRepoDetail({
  repo,
  owner,
  busy,
  offline,
  ask,
}: {
  repo: RepoStorage
  owner: string
  busy: boolean
  offline: boolean
  ask: (c: Confirm) => void
}) {
  const queryClient = useQueryClient()
  const now = useNow()
  const [selected, setSelected] = useState<number[]>([])
  const [older, setOlder] = useState("")
  const settled = () => queryClient.invalidateQueries({ queryKey: keys.githubStorage })
  const bulk = useMutation({
    mutationFn: (body: { ids: number[] } | { older_than: string }) => api.deleteArtifacts(repo.name, body),
    onSuccess: () => {
      toast.success("Delete started")
      setSelected([])
      setOlder("")
    },
    onSettled: settled,
  })
  const one = useMutation({
    mutationFn: (a: ArtifactItem) => api.deleteArtifact(repo.name, a.id),
    onSuccess: (_d, a) => {
      toast.success(`Deleted ${a.name}`)
      setSelected((s) => s.filter((id) => id !== a.id))
    },
    onSettled: settled,
  })
  const cache = useMutation({
    mutationFn: (id: number) => api.deleteCache(repo.name, id),
    onSuccess: () => {
      toast.success("Cache deleted")
    },
    onSettled: settled,
  })
  const allCaches = useMutation({
    mutationFn: () => api.deleteCaches(repo.name, { all: true }),
    onSuccess: () => {
      toast.success("Delete started")
    },
    onSettled: settled,
  })

  const held = offline || busy
  const chosen = repo.artifacts.filter((a) => selected.includes(a.id))
  const all = repo.artifacts.length > 0 && chosen.length === repo.artifacts.length
  const toggle = (id: number) => setSelected((s) => (s.includes(id) ? s.filter((x) => x !== id) : [...s, id]))

  const preview = useMutation({ mutationFn: (age: string) => api.storagePreview(age, repo.name) })

  function askOlder() {
    const age = older.trim()
    preview.mutate(age, {
      onSuccess: (previews) => {
        const count = previews.reduce((n, p) => n + p.count, 0)
        const bytes = previews.reduce((n, p) => n + p.bytes, 0)
        if (count === 0) {
          toast(`No artifact of ${repo.name} is older than ${age}`)
          return
        }
        ask({
          title: `Delete ${repo.name}'s artifacts older than ${age}?`,
          body: `Up to ${plural(count, "artifact")} (${humanBytes(bytes)}). Skips the artifacts of runs that have not completed. A run that downloads them later fails.`,
          action: "Delete",
          destructive: true,
          run: () => bulk.mutate({ older_than: age }),
        })
      },
      onError: (err) => toast.error(errorText(err)),
    })
  }

  const cacheBytes = repo.caches.reduce((n, c) => n + c.bytes, 0)

  return (
    <div className="flex flex-col gap-4 px-4 py-3">
      <Part
        title="Artifacts"
        label={`Artifacts of ${repo.name}`}
        actions={
          <>
            <Button
              size="sm"
              variant="outline"
              disabled={held || chosen.length === 0 || bulk.isPending}
              onClick={() =>
                ask({
                  title: `Delete ${plural(chosen.length, "artifact")} from ${repo.name}?`,
                  body: `Frees ${humanBytes(chosen.reduce((n, a) => n + a.bytes, 0))}. A run that downloads them later fails.`,
                  action: "Delete",
                  destructive: true,
                  run: () => bulk.mutate({ ids: chosen.map((a) => a.id) }),
                })
              }
            >
              Delete selected
            </Button>
            <Input
              aria-label={`Delete artifacts of ${repo.name} older than`}
              className="w-20 font-mono"
              placeholder="7d"
              value={older}
              disabled={offline}
              onChange={(e) => setOlder(e.target.value)}
            />
            <Button size="sm" variant="outline" disabled={held || older.trim() === "" || bulk.isPending || preview.isPending} onClick={askOlder}>
              Delete older
            </Button>
          </>
        }
      >
        {repo.artifacts.length === 0 ? (
          <p className="text-sm text-muted-foreground">No artifacts.</p>
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead className="w-8">
                  <input
                    type="checkbox"
                    aria-label={`Select every artifact of ${repo.name}`}
                    checked={all}
                    onChange={() => setSelected(all ? [] : repo.artifacts.map((a) => a.id))}
                  />
                </TableHead>
                <TableHead>Artifact</TableHead>
                <TableHead className="text-right">Size</TableHead>
                <TableHead className={WIDE}>Created</TableHead>
                <TableHead className={WIDE}>Branch</TableHead>
                <TableHead>Run</TableHead>
                <TableHead>
                  <span className="sr-only">Actions</span>
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {repo.artifacts.map((a) => (
                <TableRow key={a.id}>
                  <TableCell>
                    <input type="checkbox" aria-label={`Select ${a.name}`} checked={selected.includes(a.id)} onChange={() => toggle(a.id)} />
                  </TableCell>
                  <th scope="row" className="py-2 text-left font-normal">
                    {a.name}
                  </th>
                  <TableCell className={NUM}>{humanBytes(a.bytes)}</TableCell>
                  <TableCell className={`whitespace-nowrap text-muted-foreground ${WIDE}`}>
                    <time dateTime={a.created_at} title={dateTime(a.created_at)}>
                      {ago(now - Date.parse(a.created_at))}
                    </time>
                  </TableCell>
                  <TableCell className={`font-mono text-xs ${WIDE}`}>{a.branch}</TableCell>
                  <TableCell className="whitespace-nowrap">
                    <RunLink repo={repo} owner={owner} artifact={a} />
                  </TableCell>
                  <TableCell className="text-right">
                    <Button
                      size="icon"
                      variant="ghost"
                      aria-label={`Delete ${a.name}`}
                      disabled={offline || one.isPending}
                      onClick={() =>
                        ask({
                          title: `Delete ${a.name}?`,
                          body: `Frees ${humanBytes(a.bytes)}. A run that downloads it later fails.`,
                          action: "Delete",
                          destructive: true,
                          run: () => one.mutate(a),
                        })
                      }
                    >
                      <Trash2 size={15} aria-hidden="true" />
                    </Button>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </Part>
      <Part
        title="Caches"
        label={`Caches of ${repo.name}`}
        actions={
          <Button
            size="sm"
            variant="outline"
            disabled={held || repo.caches.length === 0 || allCaches.isPending}
            onClick={() =>
              ask({
                title: `Delete ${plural(repo.caches.length, "cache")} of ${repo.name}?`,
                body: `Frees ${humanBytes(cacheBytes)}. Jobs rebuild what they need.`,
                action: "Delete",
                destructive: true,
                run: () => allCaches.mutate(),
              })
            }
          >
            Delete all caches
          </Button>
        }
      >
        {repo.caches.length === 0 ? (
          <p className="text-sm text-muted-foreground">No caches.</p>
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Key</TableHead>
                <TableHead className={WIDE}>Ref</TableHead>
                <TableHead className="text-right">Size</TableHead>
                <TableHead className={WIDE}>Last used</TableHead>
                <TableHead>
                  <span className="sr-only">Actions</span>
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {repo.caches.map((c) => (
                <TableRow key={c.id}>
                  <th scope="row" className="py-2 text-left font-mono text-xs font-normal break-all whitespace-normal">
                    {c.key}
                  </th>
                  <TableCell className={`font-mono text-xs ${WIDE}`}>{c.ref}</TableCell>
                  <TableCell className={NUM}>{humanBytes(c.bytes)}</TableCell>
                  <TableCell className={`whitespace-nowrap text-muted-foreground ${WIDE}`}>
                    <time dateTime={c.last_used_at} title={dateTime(c.last_used_at)}>
                      {ago(now - Date.parse(c.last_used_at))}
                    </time>
                  </TableCell>
                  <TableCell className="text-right">
                    <Button
                      size="icon"
                      variant="ghost"
                      aria-label={`Delete cache ${c.key}`}
                      disabled={offline || cache.isPending}
                      onClick={() =>
                        ask({
                          title: "Delete this cache?",
                          body: `${c.key} on ${c.ref}, ${humanBytes(c.bytes)}. The next job that needs it rebuilds it.`,
                          action: "Delete",
                          destructive: true,
                          run: () => cache.mutate(c.id),
                        })
                      }
                    >
                      <Trash2 size={15} aria-hidden="true" />
                    </Button>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </Part>
    </div>
  )
}
