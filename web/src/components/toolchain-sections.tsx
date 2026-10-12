import { useMutation, useQueryClient } from "@tanstack/react-query"
import { Button } from "darkraise-ui/components/button"
import { toast } from "darkraise-ui/components/sonner"
import { Spinner } from "darkraise-ui/components/spinner"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "darkraise-ui/components/table"
import { Tooltip, TooltipContent, TooltipTrigger } from "darkraise-ui/components/tooltip"
import { Trash2 } from "lucide-react"
import { useState, type ReactNode } from "react"
import { api } from "@/api/client"
import { keys } from "@/api/hooks"
import type { Status, Storage, Toolchain } from "@/api/types"
import { ConfirmDialog, type Confirm } from "@/components/confirm-dialog"
import { Section } from "@/components/page/section"
import { RefusedHint } from "@/components/refused-hint"
import { ago, dateTime, humanBytes, plural } from "@/lib/format"
import { cacheParts, groupByTool, lastDotnetMajor, queueText, toolName, type CachePart } from "@/lib/toolchains"
import { useNow } from "@/lib/use-now"
import { usePopularSet } from "@/lib/use-popular-set"

const SLOT_CLASS: Record<CachePart["slot"], string> = {
  1: "bg-chart-1",
  2: "bg-chart-2",
  3: "bg-chart-3",
  4: "bg-chart-4",
  5: "bg-chart-5",
}

const OTHER_KEY = "__other"

function CacheBar({ parts }: { parts: CachePart[] }) {
  const total = parts.reduce((n, p) => n + p.bytes, 0)
  return (
    <div
      role="img"
      aria-label={`Tool cache: ${parts.map((p) => `${p.label} ${humanBytes(p.bytes)}`).join(", ")}`}
      className="flex h-2 overflow-hidden rounded-[3px] bg-muted"
    >
      {parts.map((p) => (
        <span key={p.key} data-part={p.key} className={SLOT_CLASS[p.slot]} style={{ width: `${(p.bytes / total) * 100}%` }} />
      ))}
    </div>
  )
}

// One tool's heading row: its swatch matches its part of the cache bar, and
// its total sits in the size column.
function GroupRow({ slot, title, count, bytes }: { slot: CachePart["slot"]; title: string; count: string; bytes: number }) {
  return (
    <TableRow className="bg-muted/40 hover:bg-muted/40">
      <th scope="rowgroup" className="px-4 py-2 text-left font-normal">
        <span className="flex items-center gap-2">
          <span aria-hidden="true" className={`size-2.5 shrink-0 rounded-[2px] ${SLOT_CLASS[slot]}`} />
          <span className="font-semibold">{title}</span>
          <span className="text-xs text-muted-foreground">{count}</span>
        </span>
      </th>
      <TableCell className="text-right font-mono font-semibold">{humanBytes(bytes)}</TableCell>
      <TableCell colSpan={2} />
    </TableRow>
  )
}

export function InstalledSection({ storage, status, offline }: { storage: Storage; status: Status | undefined; offline: boolean }) {
  const queryClient = useQueryClient()
  const popular = usePopularSet()
  const now = useNow()
  const [confirm, setConfirm] = useState<Confirm | null>(null)
  const remove = useMutation({
    mutationFn: (t: Toolchain) => api.removeToolchain(t.tool, t.version),
    onSuccess: (_data, t) => {
      toast.success(`Queued: remove ${t.tool} ${t.version}`)
    },
    onSettled: () => queryClient.invalidateQueries({ queryKey: keys.storage }),
  })
  const tools = storage.toolchains ?? []
  const others = storage.other_tool_cache ?? []
  const queue = queueText(storage.operations)
  const parts = cacheParts(storage)
  const slot = (key: string) => parts.find((p) => p.key === key)?.slot ?? 5
  const total = parts.reduce((n, p) => n + p.bytes, 0)

  function askRemove(t: Toolchain) {
    const major = lastDotnetMajor(t, tools)
    let body = `Frees ${humanBytes(t.bytes)}. Jobs that need it install it again.`
    if (major) body += ` It is the last .NET ${major} SDK, so this also removes the ${major}.0 runtimes and packs.`
    setConfirm({ title: `Remove ${t.tool} ${t.version}?`, body, action: "Remove", destructive: true, run: () => remove.mutate(t) })
  }

  const aside: ReactNode = (
    <>
      {queue && (
        <span className="flex items-center gap-2 text-sm">
          <Spinner aria-hidden="true" />
          {queue}
        </span>
      )}
      <RefusedHint what="Remove" status={status} />
      {total > 0 && <span className="ml-auto font-mono text-sm text-muted-foreground">{humanBytes(total)}</span>}
    </>
  )

  return (
    <Section title="Installed" aside={aside} flush>
      {tools.length === 0 && others.length === 0 ? (
        <div className="flex flex-col items-start gap-2 p-4">
          <p className="text-sm text-muted-foreground">The tool cache is empty</p>
          <Button variant="outline" disabled={offline} onClick={popular.ask}>
            Install popular set
          </Button>
        </div>
      ) : (
        <>
          {total > 0 && (
            <div className="px-4 pt-4 pb-3">
              <CacheBar parts={parts} />
            </div>
          )}
          {/* The card is the frame, so the table drops its own border. */}
          <div className="overflow-x-auto [&_.dr-table-wrapper]:border-0">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead className="pl-4">Version</TableHead>
                  <TableHead className="text-right">Size</TableHead>
                  <TableHead>Installed</TableHead>
                  <TableHead className="pr-4">
                    <span className="sr-only">Actions</span>
                  </TableHead>
                </TableRow>
              </TableHeader>
              {groupByTool(tools).map((g) => (
                <TableBody key={g.tool}>
                  <GroupRow
                    slot={slot(g.tool)}
                    title={toolName(g.tool)}
                    count={plural(g.rows.length, "version")}
                    bytes={g.rows.reduce((n, t) => n + t.bytes, 0)}
                  />
                  {g.rows.map((t) => (
                    <TableRow key={`${t.tool}/${t.version}/${t.arch}`}>
                      <TableCell className="pl-8 font-mono">{t.version}</TableCell>
                      <TableCell className="text-right font-mono">{humanBytes(t.bytes)}</TableCell>
                      <TableCell className="text-muted-foreground">
                        <time dateTime={t.installed_at} title={dateTime(t.installed_at)}>
                          {ago(now - Date.parse(t.installed_at))}
                        </time>
                      </TableCell>
                      <TableCell className="pr-4 text-right">
                        <Tooltip>
                          <TooltipTrigger asChild>
                            <Button size="icon" variant="ghost" aria-label={`Remove ${t.tool} ${t.version}`} disabled={offline} onClick={() => askRemove(t)}>
                              <Trash2 size={15} aria-hidden="true" />
                            </Button>
                          </TooltipTrigger>
                          <TooltipContent>Remove</TooltipContent>
                        </Tooltip>
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              ))}
              {others.length > 0 && (
                <TableBody>
                  <GroupRow
                    slot={slot(OTHER_KEY)}
                    title="Installed by jobs"
                    count="ghr does not manage these"
                    bytes={others.reduce((n, f) => n + f.bytes, 0)}
                  />
                  {others.map((f) => (
                    <TableRow key={f.name}>
                      <TableCell className="pl-8">{f.name}</TableCell>
                      <TableCell className="text-right font-mono">{humanBytes(f.bytes)}</TableCell>
                      <TableCell colSpan={2} />
                    </TableRow>
                  ))}
                </TableBody>
              )}
            </Table>
          </div>
        </>
      )}
      <ConfirmDialog confirm={confirm} onClose={() => setConfirm(null)} />
      <ConfirmDialog confirm={popular.confirm} onClose={popular.close} />
    </Section>
  )
}
