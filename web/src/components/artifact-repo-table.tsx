import { useMutation, useQueryClient } from "@tanstack/react-query"
import { Button } from "darkraise-ui/components/button"
import { Input } from "darkraise-ui/components/input"
import { toast } from "darkraise-ui/components/sonner"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "darkraise-ui/components/table"
import { Pencil } from "lucide-react"
import { useState } from "react"
import { api } from "@/api/client"
import { keys } from "@/api/hooks"
import type { Config, GitHubStorage, RepoStorage } from "@/api/types"
import { ConfirmDialog, type Confirm } from "@/components/confirm-dialog"
import { Section } from "@/components/page/section"
import { CleanupPreview } from "@/components/storage-policy-card"
import { humanBytes, plural } from "@/lib/format"
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

export function ArtifactRepoTable({ storage, config, offline }: { storage: GitHubStorage; config: Config | undefined; offline: boolean }) {
  const [confirm, setConfirm] = useState<Confirm | null>(null)
  const globalAge = config?.github_storage?.cleanup_max_age ?? ""
  const warned = (storage.quota?.level ?? "") !== ""
  return (
    <Section id="repositories" title="Repositories" flush>
      {storage.repos.length === 0 ? (
        <p className="p-4 text-sm text-muted-foreground">No repositories listed yet.</p>
      ) : (
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
              {storage.repos.map((r) => (
                <TableRow key={r.name}>
                  <th scope="row" className="py-2 pl-4 text-left align-top font-normal">
                    <span className="flex items-center gap-2">
                      {r.name}{" "}
                      {r.visibility === "public" && <span className="text-xs text-muted-foreground">public</span>}
                    </span>
                    {warned && r.retention_days === 90 && (
                      <p className="text-xs text-warning">At GitHub's 90-day default: lowering it saves storage.</p>
                    )}
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
                    <CleanupCell repo={r} globalAge={globalAge} offline={offline} ask={setConfirm} />
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      )}
      <ConfirmDialog confirm={confirm} onClose={() => setConfirm(null)} />
    </Section>
  )
}
