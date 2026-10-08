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
import { StateText } from "@/components/page/state-text"
import { RefusedHint } from "@/components/refused-hint"
import { SizeBar } from "@/components/size-bar"
import { ago, hhmm, humanBytes } from "@/lib/format"
import { useNow } from "@/lib/use-now"

export function PackageCachesSection({
  storage,
  status,
  offline,
  className = "",
}: {
  storage: Storage
  status: Status | undefined
  offline: boolean
  className?: string
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
  const longest = Math.max(1, ...present.map((c) => c.bytes))
  const aside = (
    <span className="ml-auto flex items-center gap-2 text-sm text-muted-foreground">
      {storage.measuring ? (
        <Spinner label="Measuring" />
      ) : (
        <span>{storage.measured_at ? `Measured ${hhmm(new Date(storage.measured_at))}` : "Not measured yet"}</span>
      )}
      <Button size="sm" variant="outline" disabled={offline || storage.measuring} onClick={() => refresh.mutate()}>
        Refresh
      </Button>
    </span>
  )

  return (
    <Section title="Package caches" aside={aside} className={className}>
      <div className="flex flex-col gap-3">
        {present.length > 0 && (
          <div className="overflow-x-auto">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Cache</TableHead>
                  <TableHead>Paths</TableHead>
                  <TableHead className="text-right">Size</TableHead>
                  <TableHead className="text-right">Files</TableHead>
                  <TableHead>Last written</TableHead>
                  <TableHead>
                    <span className="sr-only">Actions</span>
                  </TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {present.map((c) => (
                  <TableRow key={c.name}>
                    <TableCell>{c.label}</TableCell>
                    <TableCell className="whitespace-normal">
                      <span className="font-mono text-xs break-all text-muted-foreground">{(c.paths ?? []).join(" ")}</span>
                    </TableCell>
                    <TableCell>
                      <SizeBar bytes={c.bytes} longest={longest} />
                    </TableCell>
                    <TableCell className="text-right font-mono">{c.files}</TableCell>
                    <TableCell>{c.last_written ? ago(now - Date.parse(c.last_written)) : "Never written"}</TableCell>
                    <TableCell className="text-right">
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
                                body: `Frees ${humanBytes(c.bytes)}. Jobs download what they need again.`,
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
        {absent.length > 0 && <p className="text-sm text-muted-foreground">{`Not present: ${absent.map((c) => c.label).join(", ")}`}</p>}
        <RefusedHint what="Clear" status={status} />
        {storage.measure_error && <ErrorLine>{storage.measure_error}</ErrorLine>}
      </div>
      <ConfirmDialog confirm={confirm} onClose={() => setConfirm(null)} />
    </Section>
  )
}

export function OperationsSection({ storage, className = "" }: { storage: Storage; className?: string }) {
  const recent = storage.operations.recent ?? []
  return (
    <Section title="Recent operations" className={className}>
      {recent.length === 0 ? (
        <p className="text-sm text-muted-foreground">No operations since the daemon started.</p>
      ) : (
        <div className="overflow-x-auto">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Time</TableHead>
                <TableHead>Kind</TableHead>
                <TableHead>Target</TableHead>
                <TableHead>Outcome</TableHead>
                <TableHead>Message</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {recent.map((o) => (
                <TableRow key={o.id}>
                  <TableCell className="font-mono">{hhmm(new Date(o.finished_at ?? o.started_at))}</TableCell>
                  <TableCell>{o.kind}</TableCell>
                  <TableCell>{o.target}</TableCell>
                  <TableCell>{o.outcome && <StateText state={o.outcome} label={o.outcome === "ok" ? "OK" : undefined} />}</TableCell>
                  <TableCell className="whitespace-normal text-muted-foreground">{o.message}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      )}
    </Section>
  )
}
