import { Spinner } from "darkraise-ui/components/spinner"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "darkraise-ui/components/table"
import type { Status, Storage } from "@/api/types"
import { DiskBreakdown } from "@/components/disk-breakdown"
import { Section } from "@/components/page/section"
import { humanBytes } from "@/lib/format"
import { lastPruneText } from "@/lib/storage"

const NUM = "text-right font-mono"

function LastPrune({ storage, status }: { storage: Storage; status: Status }) {
  const p = storage.last_prune
  if (status.maintenance.running || (p && !p.finished_at)) return <Spinner label="Pruning" />
  if (!p?.finished_at) return <p className="text-sm text-muted-foreground">No prune since the daemon started</p>
  return (
    <div className="flex flex-col gap-1 text-sm">
      <p>{lastPruneText(p)}</p>
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

export function DiskSection({ storage, status, highWater, className = "" }: { storage: Storage; status: Status; highWater: number; className?: string }) {
  const d = storage.docker
  return (
    <Section
      title="Disk"
      className={className}
      aside={status.disk_root ? <span className="font-mono text-sm text-muted-foreground">{status.disk_root}</span> : undefined}
    >
      <div className="flex flex-col gap-4">
        <DiskBreakdown status={status} storage={storage} highWater={highWater} />
        <div className="overflow-x-auto">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Type</TableHead>
                <TableHead className="text-right">Count</TableHead>
                <TableHead className="text-right">Active</TableHead>
                <TableHead className="text-right">Size</TableHead>
                <TableHead className="text-right">Reclaimable</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {(d.rows ?? []).flatMap((r) => [
                <TableRow key={r.type}>
                  <TableCell>{r.type}</TableCell>
                  <TableCell className={NUM}>{r.count}</TableCell>
                  <TableCell className={NUM}>{r.active}</TableCell>
                  <TableCell className={NUM}>{humanBytes(r.bytes)}</TableCell>
                  <TableCell className={NUM}>{humanBytes(r.reclaimable)}</TableCell>
                </TableRow>,
                ...(r.type === "Build Cache" ? (d.build_cache_types ?? []) : []).map((t) => (
                  <TableRow key={`build-cache-${t.type}`} className="text-muted-foreground">
                    <TableCell className="pl-6">{t.type}</TableCell>
                    <TableCell className={NUM}>{t.count}</TableCell>
                    <TableCell />
                    <TableCell className={NUM}>{humanBytes(t.bytes)}</TableCell>
                    <TableCell className={NUM}>{humanBytes(t.reclaimable)}</TableCell>
                  </TableRow>
                )),
              ])}
            </TableBody>
          </Table>
        </div>
        <LastPrune storage={storage} status={status} />
      </div>
    </Section>
  )
}
