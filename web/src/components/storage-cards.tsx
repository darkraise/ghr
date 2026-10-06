import { useMutation, useQueryClient } from "@tanstack/react-query"
import { Badge, type BadgeVariant } from "darkraise-ui/components/badge"
import { Button } from "darkraise-ui/components/button"
import { Card, CardContent, CardHeader, CardTitle } from "darkraise-ui/components/card"
import { toast } from "darkraise-ui/components/sonner"
import { Table, TableBody, TableCell, TableRow } from "darkraise-ui/components/table"
import { useEffect, useRef, useState } from "react"
import { api } from "@/api/client"
import { keys } from "@/api/hooks"
import type { PackageCache, Status, Storage } from "@/api/types"
import { ConfirmDialog, type Confirm } from "@/components/confirm-dialog"
import { RefusedHint } from "@/components/refused-hint"
import { ago, hhmm, humanBytes } from "@/lib/format"
import { useNow } from "@/lib/use-now"

const OUTCOME: Record<string, BadgeVariant> = { ok: "green", refused: "amber", interrupted: "amber", failed: "red" }

export function PackageCachesCard({ storage, status, offline }: { storage: Storage; status: Status | undefined; offline: boolean }) {
  const queryClient = useQueryClient()
  const now = useNow()
  const [confirm, setConfirm] = useState<Confirm | null>(null)
  const settled = () => queryClient.invalidateQueries({ queryKey: keys.storage })
  const clear = useMutation({
    mutationFn: (c: PackageCache) => api.clearCache(c.name),
    onSuccess: (_data, c) => {
      toast.success(`queued: clear ${c.label}`)
    },
    onSettled: settled,
  })
  const refresh = useMutation({
    mutationFn: () => api.refreshStorage(),
    onSuccess: () => {
      toast.success("measurement started")
    },
    onSettled: settled,
  })
  const measured = storage.measuring ? (
    <span className="text-amber-600">measuring…</span>
  ) : storage.measured_at ? (
    `measured ${hhmm(new Date(storage.measured_at))}`
  ) : (
    "not measured yet"
  )

  return (
    <Card>
      <CardHeader>
        <CardTitle>Package caches</CardTitle>
      </CardHeader>
      <CardContent className="flex flex-col gap-3">
        <Table>
          <TableBody>
            {(storage.package_caches ?? []).map((c) =>
              c.present ? (
                <TableRow key={c.name}>
                  <TableCell>{c.label}</TableCell>
                  <TableCell className="max-w-48 truncate text-muted-foreground">{(c.paths ?? []).join(" ")}</TableCell>
                  <TableCell>{humanBytes(c.bytes)}</TableCell>
                  <TableCell>{`${c.files} files`}</TableCell>
                  <TableCell className="text-muted-foreground">
                    {c.last_written ? `written ${ago(now - Date.parse(c.last_written))}` : "never written"}
                  </TableCell>
                  <TableCell className="text-right">
                    <Button
                      size="sm"
                      variant="destructive"
                      aria-label={`Clear ${c.label}`}
                      disabled={offline}
                      onClick={() =>
                        setConfirm({
                          title: `Clear the ${c.label} cache (${humanBytes(c.bytes)})? Jobs download what they need again.`,
                          action: "Clear",
                          destructive: true,
                          run: () => clear.mutate(c),
                        })
                      }
                    >
                      Clear
                    </Button>
                  </TableCell>
                </TableRow>
              ) : (
                <TableRow key={c.name}>
                  <TableCell>{c.label}</TableCell>
                  <TableCell colSpan={5} className="text-muted-foreground">
                    not present
                  </TableCell>
                </TableRow>
              ),
            )}
          </TableBody>
        </Table>
        <RefusedHint what="Clear" status={status} />
        <div className="flex items-center justify-end gap-2 text-sm text-muted-foreground">
          <span>{measured}</span>
          <Button size="sm" variant="outline" disabled={offline || storage.measuring} onClick={() => refresh.mutate()}>
            Refresh
          </Button>
        </div>
        {storage.measure_error && <p className="text-sm text-destructive">✖ {storage.measure_error}</p>}
      </CardContent>
      <ConfirmDialog confirm={confirm} onClose={() => setConfirm(null)} />
    </Card>
  )
}

export function OperationsCard({ storage }: { storage: Storage }) {
  const recent = storage.operations.recent ?? []
  return (
    <Card>
      <CardHeader>
        <CardTitle>Recent operations</CardTitle>
      </CardHeader>
      <CardContent>
        {recent.length === 0 ? (
          <p className="text-sm text-muted-foreground">no operations since the daemon started</p>
        ) : (
          <Table>
            <TableBody>
              {recent.map((o) => (
                <TableRow key={o.id}>
                  <TableCell>{hhmm(new Date(o.finished_at ?? o.started_at))}</TableCell>
                  <TableCell>{o.kind}</TableCell>
                  <TableCell>{o.target}</TableCell>
                  <TableCell>
                    <Badge variant={OUTCOME[o.outcome ?? ""] ?? "secondary"} size="sm">
                      {o.outcome}
                    </Badge>
                  </TableCell>
                  <TableCell className="text-muted-foreground">{o.message}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </CardContent>
    </Card>
  )
}

// One toast per snapshot names every operation that finished since the last
// one, failures first. The first snapshot only records where the page starts,
// so opening it does not replay old results.
export function useOperationToasts(storage: Storage | undefined) {
  const last = useRef<string | null | undefined>(undefined)
  useEffect(() => {
    if (!storage) return
    const recent = storage.operations.recent ?? []
    const newest = recent[0]?.id ?? null
    if (last.current === undefined || newest === null || newest === last.current) {
      if (last.current === undefined) last.current = newest
      return
    }
    const seen = recent.findIndex((o) => o.id === last.current)
    const fresh = seen === -1 ? recent : recent.slice(0, seen)
    last.current = newest
    const bad: string[] = []
    const good: string[] = []
    for (const o of fresh) {
      const text = `${o.kind} ${o.target}: ${o.message || o.outcome || ""}`
      if (o.outcome === "ok" || o.outcome === "skipped") good.push(text)
      else bad.push(text)
    }
    const text = [...bad, ...good].join(" · ")
    if (bad.length > 0) toast.error(text)
    else toast.success(text)
  }, [storage])
}
