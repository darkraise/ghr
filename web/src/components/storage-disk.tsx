import { Link } from "@tanstack/react-router"
import { Button } from "darkraise-ui/components/button"
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "darkraise-ui/components/dropdown-menu"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "darkraise-ui/components/table"
import { Ellipsis } from "lucide-react"
import type { Config, Status, Storage } from "@/api/types"
import { ConfirmDialog } from "@/components/confirm-dialog"
import { DiskBar } from "@/components/disk-breakdown"
import { Section } from "@/components/page/section"
import { RefusedHint } from "@/components/refused-hint"
import { diskParts, partTint, type DiskPart } from "@/lib/disk"
import { humanBytes } from "@/lib/format"
import { busyJobs } from "@/lib/status"
import { pruneActions, type PruneAction } from "@/lib/storage"
import { usePrune } from "@/lib/use-prune"

const NUM = "text-right font-mono whitespace-nowrap"
// On a phone Count and Active give way, and Reclaimable moves under Size.
const WIDE = "hidden sm:table-cell"

function PartName({ part }: { part: DiskPart }) {
  return (
    <th scope="row" className="px-4 py-2 text-left font-normal whitespace-nowrap">
      <span className="flex items-center gap-2">
        <span aria-hidden="true" className={`size-2.5 shrink-0 rounded-[2px] ${partTint(part.key)}`} />
        {part.key === "toolchains" ? (
          <Link to="/settings/$section" params={{ section: "toolchains" }} className="underline-offset-4 hover:underline">
            {part.label}
          </Link>
        ) : (
          part.label
        )}
      </span>
    </th>
  )
}

// Buttons from 640px up; below that the row keeps one menu so the sizes stay
// on screen.
function RowActions({
  part,
  actions,
  disabled,
  onAsk,
}: {
  part: DiskPart
  actions: PruneAction[]
  disabled: boolean
  onAsk: (a: PruneAction) => void
}) {
  if (actions.length === 0) return null
  return (
    <>
      <span className="hidden justify-end gap-2 sm:flex">
        {actions.map((a) => (
          <Button key={a.scope} size="sm" variant="outline" aria-label={a.action} disabled={disabled} onClick={() => onAsk(a)}>
            {a.label}
          </Button>
        ))}
      </span>
      <span className="flex justify-end sm:hidden">
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button size="icon" variant="ghost" aria-label={`Prune ${part.label.toLowerCase()}`} disabled={disabled}>
              <Ellipsis size={15} aria-hidden="true" />
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end">
            {actions.map((a) => (
              <DropdownMenuItem key={a.scope} onSelect={() => onAsk(a)}>
                {a.action}
              </DropdownMenuItem>
            ))}
          </DropdownMenuContent>
        </DropdownMenu>
      </span>
    </>
  )
}

export function DiskSection({
  storage,
  status,
  config,
  highWater,
  disabled,
}: {
  storage: Storage
  status: Status
  config: Config | undefined
  highWater: number
  disabled: boolean
}) {
  const prune = usePrune()
  const measured = status.disk_total_bytes > 0
  const parts = diskParts(storage, status.disk_used_bytes)
  const actions = pruneActions(storage, config)
  const refused = busyJobs(status) > 0
  const docker = (type: string | undefined) => (storage.docker.rows ?? []).find((r) => r.type === type)
  const aside = (
    <>
      {status.disk_root && <span className="font-mono text-sm text-muted-foreground">{status.disk_root}</span>}
      {measured && (
        <span className="ml-auto font-mono text-sm text-muted-foreground">{`${humanBytes(status.disk_used_bytes)} of ${humanBytes(status.disk_total_bytes)}`}</span>
      )}
    </>
  )

  return (
    <Section id="prune" title="Disk" aside={aside} flush>
      <div className="px-4 pt-4 pb-3">
        {measured ? <DiskBar status={status} storage={storage} highWater={highWater} /> : <p className="text-sm text-muted-foreground">Not measured yet</p>}
      </div>
      {/* The card is the frame, so the table drops its own border. */}
      <div className="overflow-x-auto [&_.dr-table-wrapper]:border-0">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead className="pl-4">Type</TableHead>
              <TableHead className={`text-right ${WIDE}`}>Count</TableHead>
              <TableHead className={`text-right ${WIDE}`}>Active</TableHead>
              <TableHead className="text-right">Size</TableHead>
              <TableHead className={`text-right ${WIDE}`}>Reclaimable</TableHead>
              <TableHead className="pr-4">
                <span className="sr-only">Actions</span>
              </TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {parts.flatMap((part) => {
              const row = docker(part.docker)
              const own = actions.filter((a) => a.type === part.docker)
              return [
                <TableRow key={part.key} className={part.docker === "Local Volumes" && refused ? "border-b-0" : undefined}>
                  <PartName part={part} />
                  <TableCell className={`${NUM} ${WIDE}`}>{row?.count}</TableCell>
                  <TableCell className={`${NUM} ${WIDE}`}>{row?.active}</TableCell>
                  <TableCell className={NUM}>
                    {humanBytes(part.bytes)}
                    {row && <div className="font-sans text-xs whitespace-normal text-muted-foreground sm:hidden">{`${humanBytes(row.reclaimable)} reclaimable`}</div>}
                  </TableCell>
                  <TableCell className={`${NUM} ${WIDE}`}>{row && humanBytes(row.reclaimable)}</TableCell>
                  <TableCell className="pr-4">
                    <RowActions part={part} actions={own} disabled={disabled} onAsk={(a) => prune.ask(a.scope, a)} />
                  </TableCell>
                </TableRow>,
                ...(part.docker === "Local Volumes" && refused
                  ? [
                      <TableRow key="volumes-refused" className="hover:bg-transparent">
                        <TableCell colSpan={6} className="px-4 pt-0 whitespace-normal">
                          <RefusedHint what="Unused volumes" status={status} />
                        </TableCell>
                      </TableRow>,
                    ]
                  : []),
                ...(part.docker === "Build Cache" ? (storage.docker.build_cache_types ?? []) : []).map((t) => (
                  <TableRow key={`build-cache-${t.type}`} className="text-muted-foreground">
                    <TableCell className="pl-8 sm:pl-12">{t.type}</TableCell>
                    <TableCell className={`${NUM} ${WIDE}`}>{t.count}</TableCell>
                    <TableCell className={WIDE} />
                    <TableCell className={NUM}>{humanBytes(t.bytes)}</TableCell>
                    <TableCell className={`${NUM} ${WIDE}`}>{humanBytes(t.reclaimable)}</TableCell>
                    <TableCell />
                  </TableRow>
                )),
              ]
            })}
          </TableBody>
        </Table>
      </div>
      <ConfirmDialog confirm={prune.confirm} onClose={prune.close} />
    </Section>
  )
}
