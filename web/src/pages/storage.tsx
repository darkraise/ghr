import { useMutation, useQueryClient } from "@tanstack/react-query"
import { Button } from "darkraise-ui/components/button"
import { Card, CardContent, CardHeader, CardTitle } from "darkraise-ui/components/card"
import { Progress } from "darkraise-ui/components/progress"
import { toast } from "darkraise-ui/components/sonner"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "darkraise-ui/components/table"
import { PageHeader } from "darkraise-ui/layout"
import { useState } from "react"
import { api } from "@/api/client"
import { keys, useConfig, useStatus, useStorage } from "@/api/hooks"
import type { Config, DockerDisk, PruneScope, Status, Storage } from "@/api/types"
import { ConfirmDialog, type Confirm } from "@/components/confirm-dialog"
import { RefusedHint } from "@/components/refused-hint"
import { hhmm, humanBytes } from "@/lib/format"
import { errorText } from "@/query"

function reclaim(d: DockerDisk, type: string): string {
  const row = (d.rows ?? []).find((r) => r.type === type && r.reclaimable > 0)
  return row ? ` (up to ${humanBytes(row.reclaimable)})` : ""
}

interface PruneButton {
  scope: PruneScope
  label: string
  variant: "secondary" | "destructive" | undefined
  question: string
}

function pruneButtons(storage: Storage, config: Config | undefined): PruneButton[] {
  const keep = config?.build_cache_keep
  const size = keep ?? "the build_cache_keep size"
  const d = storage.docker
  return [
    {
      scope: "standard",
      label: "Prune",
      variant: undefined,
      question: `Prune now? Removes build cache beyond ${size}, dangling images, and history and logs past retention.`,
    },
    { scope: "build-cache-keep", label: `Build cache to ${keep ?? "keep"}`, variant: "secondary", question: `Prune the build cache down to ${size}?` },
    {
      scope: "build-cache-all",
      label: "All build cache",
      variant: "destructive",
      question: `Remove all build cache${reclaim(d, "Build Cache")}? The next builds start cold.`,
    },
    { scope: "dangling-images", label: "Dangling images", variant: "secondary", question: "Remove dangling images (untagged and used by no container)?" },
    {
      scope: "unused-volumes",
      label: "Unused volumes",
      variant: "destructive",
      question: `Remove every volume no container uses${reclaim(d, "Local Volumes")}? It is refused while jobs run.`,
    },
  ]
}

function LastPrune({ storage, status }: { storage: Storage; status: Status | undefined }) {
  const p = storage.last_prune
  if (status?.maintenance.running || (p && !p.finished_at)) return <p className="text-sm text-amber-600">pruning…</p>
  if (!p?.finished_at) return <p className="text-sm text-muted-foreground">no prune since start</p>
  const head = p.trigger === "auto" ? p.trigger : `${p.trigger} ${p.scope}`
  const steps = p.steps ?? []
  return (
    <p className="text-sm">
      {`${head} · ${hhmm(new Date(p.finished_at))} · ${p.outcome ?? ""}`}
      {steps.length > 0 && " — "}
      {steps.map((st, i) => (
        <span key={st.name} className={st.error ? "text-destructive" : undefined}>
          {i > 0 && ", "}
          {st.error ? `${st.name}: ${st.error}` : `${st.name} ${humanBytes(st.freed)}`}
        </span>
      ))}
    </p>
  )
}

function DockerCard({
  storage,
  status,
  config,
  offline,
}: {
  storage: Storage
  status: Status | undefined
  config: Config | undefined
  offline: boolean
}) {
  const queryClient = useQueryClient()
  const [confirm, setConfirm] = useState<Confirm | null>(null)
  const prune = useMutation({
    mutationFn: (scope: PruneScope) => api.prune(scope),
    onSuccess: (_data, scope) => {
      toast.success(`${scope} prune started`)
    },
    onSettled: () =>
      Promise.all([
        queryClient.invalidateQueries({ queryKey: keys.storage }),
        queryClient.invalidateQueries({ queryKey: keys.status }),
      ]),
  })
  const d = storage.docker
  const disabled = offline || (status?.maintenance.running ?? false)
  return (
    <Card>
      <CardHeader>
        <CardTitle>Docker disk</CardTitle>
      </CardHeader>
      <CardContent className="flex flex-col gap-3">
        <Progress value={d.disk_pct} aria-label="disk use" />
        <p className="text-sm">
          {d.disk_pct}% used · prunes above {config?.disk_high_water ?? 80}%
        </p>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead />
              <TableHead>count</TableHead>
              <TableHead>active</TableHead>
              <TableHead>size</TableHead>
              <TableHead>reclaimable</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {(d.rows ?? []).flatMap((r) => [
              <TableRow key={r.type}>
                <TableCell>{r.type}</TableCell>
                <TableCell>{r.count}</TableCell>
                <TableCell>{r.active}</TableCell>
                <TableCell>{humanBytes(r.bytes)}</TableCell>
                <TableCell>{humanBytes(r.reclaimable)}</TableCell>
              </TableRow>,
              ...(r.type === "Build Cache" ? (d.build_cache_types ?? []) : []).map((t) => (
                <TableRow key={`build-cache-${t.type}`} className="text-muted-foreground">
                  <TableCell className="pl-6">{t.type}</TableCell>
                  <TableCell>{t.count}</TableCell>
                  <TableCell />
                  <TableCell>{humanBytes(t.bytes)}</TableCell>
                  <TableCell>{humanBytes(t.reclaimable)}</TableCell>
                </TableRow>
              )),
            ])}
          </TableBody>
        </Table>
        <LastPrune storage={storage} status={status} />
        <div className="flex flex-wrap gap-2">
          {pruneButtons(storage, config).map((b) => (
            <Button
              key={b.scope}
              size="sm"
              variant={b.variant}
              disabled={disabled}
              onClick={() =>
                setConfirm({ title: b.question, action: b.label, destructive: b.variant === "destructive", run: () => prune.mutate(b.scope) })
              }
            >
              {b.label}
            </Button>
          ))}
        </div>
        <RefusedHint what="Unused volumes" status={status} />
      </CardContent>
      <ConfirmDialog confirm={confirm} onClose={() => setConfirm(null)} />
    </Card>
  )
}

export function StoragePage() {
  const status = useStatus()
  const config = useConfig()
  const storage = useStorage(status.data)
  const offline = status.isError
  const data = storage.data
  return (
    <>
      <PageHeader title="Storage" />
      {storage.isError && <p className="mb-4 text-sm text-destructive">✖ {errorText(storage.error)}</p>}
      {data ? (
        <div className="grid gap-4 xl:grid-cols-2">
          <DockerCard storage={data} status={status.data} config={config.data} offline={offline} />
        </div>
      ) : (
        !storage.isError && <p className="text-sm text-muted-foreground">loading…</p>
      )}
    </>
  )
}
