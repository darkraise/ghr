import { useMutation, useQueryClient } from "@tanstack/react-query"
import { Button } from "darkraise-ui/components/button"
import { toast } from "darkraise-ui/components/sonner"
import { Spinner } from "darkraise-ui/components/spinner"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "darkraise-ui/components/table"
import { Tooltip, TooltipContent, TooltipTrigger } from "darkraise-ui/components/tooltip"
import { Eraser } from "lucide-react"
import { useState } from "react"
import { api } from "@/api/client"
import { keys } from "@/api/hooks"
import type { PackageCache, Status, Storage } from "@/api/types"
import { ConfirmDialog, type Confirm } from "@/components/confirm-dialog"
import { ErrorLine } from "@/components/page/error-line"
import { Section } from "@/components/page/section"
import { StatusPill } from "@/components/page/status-pill"
import { RefusedHint } from "@/components/refused-hint"
import { ago, dateTime, humanBytes, shortWhen } from "@/lib/format"
import { lastPruneText, MAINTENANCE_HOLD } from "@/lib/storage"
import { queueText } from "@/lib/toolchains"
import { useNow } from "@/lib/use-now"

export function PackageCachesSection({
  storage,
  status,
  offline,
}: {
  storage: Storage
  status: Status | undefined
  offline: boolean
}) {
  const queryClient = useQueryClient()
  const now = useNow()
  const [confirm, setConfirm] = useState<Confirm | null>(null)
  const settled = () => queryClient.invalidateQueries({ queryKey: keys.storage })
  const clear = useMutation({
    mutationFn: (c: PackageCache) => api.clearCache(c.name),
    onSuccess: (_data, c) => {
      toast.success(`Queued: clear ${c.label}`)
    },
    onSettled: settled,
  })
  const refresh = useMutation({
    mutationFn: () => api.refreshStorage(),
    onSuccess: () => {
      toast.success("Measurement started")
    },
    onSettled: settled,
  })
  const all = storage.package_caches ?? []
  const present = all.filter((c) => c.present)
  const absent = all.filter((c) => !c.present)
  const aside = (
    <>
      <RefusedHint what="Clear" status={status} />
      <span className="ml-auto flex items-center gap-2 text-sm text-muted-foreground">
        {storage.measuring ? (
          <Spinner label="Measuring" />
        ) : (
          <span>{storage.measured_at ? `Measured ${shortWhen(storage.measured_at, now)}` : "Not measured yet"}</span>
        )}
        <Button size="sm" variant="outline" disabled={offline || storage.measuring} onClick={() => refresh.mutate()}>
          Refresh
        </Button>
      </span>
    </>
  )
  const notes = absent.length > 0 || storage.measure_error

  return (
    <Section id="package-caches" title="Package caches" aside={aside} flush>
      {present.length > 0 && (
        <div className="overflow-x-auto [&_.dr-table-wrapper]:border-0">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead className="pl-4">Cache</TableHead>
                <TableHead className="text-right">Size</TableHead>
                <TableHead className="hidden text-right sm:table-cell">Files</TableHead>
                <TableHead className="hidden sm:table-cell">Last written</TableHead>
                <TableHead className="pr-4">
                  <span className="sr-only">Actions</span>
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {present.map((c) => (
                <TableRow key={c.name}>
                  <TableCell className="pl-4 whitespace-normal">
                    <div>{c.label}</div>
                    <div className="font-mono text-xs break-words text-muted-foreground">{(c.paths ?? []).join(" ")}</div>
                  </TableCell>
                  <TableCell className="text-right font-mono whitespace-nowrap">{humanBytes(c.bytes)}</TableCell>
                  <TableCell className="hidden text-right font-mono sm:table-cell">{c.files.toLocaleString("en-US")}</TableCell>
                  <TableCell className="hidden whitespace-nowrap text-muted-foreground sm:table-cell">
                    {c.last_written ? (
                      <time dateTime={c.last_written} title={dateTime(c.last_written)}>
                        {ago(now - Date.parse(c.last_written))}
                      </time>
                    ) : (
                      "Never written"
                    )}
                  </TableCell>
                  <TableCell className="pr-4 text-right">
                    <Tooltip>
                      <TooltipTrigger asChild>
                        <Button
                          size="icon"
                          variant="ghost"
                          aria-label={`Clear ${c.label}`}
                          disabled={offline}
                          onClick={() =>
                            setConfirm({
                              title: `Clear ${c.label}?`,
                              body: `Frees ${humanBytes(c.bytes)}. Jobs download what they need again. ${MAINTENANCE_HOLD}`,
                              action: "Clear",
                              destructive: true,
                              run: () => clear.mutate(c),
                            })
                          }
                        >
                          <Eraser size={15} aria-hidden="true" />
                        </Button>
                      </TooltipTrigger>
                      <TooltipContent>Clear</TooltipContent>
                    </Tooltip>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      )}
      {notes && (
        <div className={`flex flex-col gap-2 p-4 ${present.length > 0 ? "border-t border-border" : ""}`}>
          {absent.length > 0 && <p className="text-sm text-muted-foreground">{`Not present: ${absent.map((c) => c.label).join(", ")}`}</p>}
          {storage.measure_error && <ErrorLine>{storage.measure_error}</ErrorLine>}
        </div>
      )}
      <ConfirmDialog confirm={confirm} onClose={() => setConfirm(null)} />
    </Section>
  )
}

function LastPrune({ storage, status, now }: { storage: Storage; status: Status; now: number }) {
  const p = storage.last_prune
  if (status.maintenance.running || (p && !p.finished_at)) return <Spinner label="Pruning" />
  if (!p?.finished_at) return <p className="text-sm text-muted-foreground">No prune since the daemon started</p>
  return (
    <div className="flex flex-col gap-1 text-sm">
      <p>{lastPruneText(p, now)}</p>
      {(p.steps ?? []).length > 0 && (
        <ul className="flex flex-col gap-0.5">
          {(p.steps ?? []).map((st) => (
            <li key={st.name} className={st.error ? "text-destructive" : "text-muted-foreground"}>
              {st.error ? `${st.name}: ${st.error}` : `${st.name}: ${humanBytes(st.freed)} freed`}
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}

export function ActivitySection({ storage, status }: { storage: Storage; status: Status }) {
  const now = useNow()
  const recent = storage.operations.recent ?? []
  const queue = queueText(storage.operations)
  const aside = queue ? (
    <span className="flex items-center gap-2 text-sm">
      <Spinner aria-hidden="true" />
      {queue}
    </span>
  ) : undefined
  return (
    <Section title="Activity" aside={aside} flush>
      <div className="border-b border-border p-4">
        <LastPrune storage={storage} status={status} now={now} />
      </div>
      {recent.length === 0 ? (
        <p className="p-4 text-sm text-muted-foreground">No operations since the daemon started.</p>
      ) : (
        <div className="overflow-x-auto [&_.dr-table-wrapper]:border-0">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead className="pl-4">Time</TableHead>
                <TableHead>Operation</TableHead>
                <TableHead className="pr-4">Outcome</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {recent.map((o) => {
                const at = o.finished_at ?? o.started_at
                return (
                  <TableRow key={o.id}>
                    <TableCell className="pl-4 font-mono whitespace-nowrap" title={dateTime(at)}>
                      {shortWhen(at, now)}
                    </TableCell>
                    <TableCell className="w-full whitespace-normal">
                      <div>{`${o.kind} ${o.target}`}</div>
                      {o.message && <div className="text-sm break-words text-muted-foreground">{o.message}</div>}
                    </TableCell>
                    <TableCell className="pr-4">{o.outcome && <StatusPill state={o.outcome} label={o.outcome === "ok" ? "OK" : undefined} />}</TableCell>
                  </TableRow>
                )
              })}
            </TableBody>
          </Table>
        </div>
      )}
    </Section>
  )
}
