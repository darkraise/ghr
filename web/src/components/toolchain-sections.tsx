import { useMutation, useQueryClient } from "@tanstack/react-query"
import { Button } from "darkraise-ui/components/button"
import { toast } from "darkraise-ui/components/sonner"
import { Spinner } from "darkraise-ui/components/spinner"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "darkraise-ui/components/table"
import { Tooltip, TooltipContent, TooltipTrigger } from "darkraise-ui/components/tooltip"
import { Trash2 } from "lucide-react"
import { useState } from "react"
import { api } from "@/api/client"
import { keys } from "@/api/hooks"
import type { Status, Storage, Toolchain } from "@/api/types"
import { ConfirmDialog, type Confirm } from "@/components/confirm-dialog"
import { Section } from "@/components/page/section"
import { RefusedHint } from "@/components/refused-hint"
import { SizeBar } from "@/components/size-bar"
import { dateTime, humanBytes } from "@/lib/format"
import { cacheParts, groupByTool, lastDotnetMajor, queueText, type CachePart } from "@/lib/toolchains"
import { usePopularSet } from "@/lib/use-popular-set"

export function InstalledSection({ storage, status, offline }: { storage: Storage; status: Status | undefined; offline: boolean }) {
  const queryClient = useQueryClient()
  const popular = usePopularSet()
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
  const longest = Math.max(1, ...tools.map((t) => t.bytes))

  function askRemove(t: Toolchain) {
    const major = lastDotnetMajor(t, tools)
    let body = `Frees ${humanBytes(t.bytes)}. Jobs that need it install it again.`
    if (major) body += ` It is the last .NET ${major} SDK, so this also removes the ${major}.0 runtimes and packs.`
    setConfirm({ title: `Remove ${t.tool} ${t.version}?`, body, action: "Remove", destructive: true, run: () => remove.mutate(t) })
  }

  return (
    <Section title="Installed">
      <div className="flex flex-col gap-3">
        {queue && (
          <p className="flex items-center gap-2 text-sm">
            <Spinner aria-hidden="true" />
            <span>{queue}</span>
          </p>
        )}
        <RefusedHint what="Remove" status={status} />
        {tools.length === 0 && others.length === 0 ? (
          <div className="flex flex-col items-start gap-2">
            <p className="text-sm text-muted-foreground">The tool cache is empty</p>
            <Button variant="outline" disabled={offline} onClick={popular.ask}>
              Install popular set
            </Button>
          </div>
        ) : (
          <>
            {tools.length > 0 && (
              <div className="overflow-x-auto">
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>Version</TableHead>
                      <TableHead>Arch</TableHead>
                      <TableHead className="text-right">Size</TableHead>
                      <TableHead>Installed</TableHead>
                      <TableHead>
                        <span className="sr-only">Actions</span>
                      </TableHead>
                    </TableRow>
                  </TableHeader>
                  {groupByTool(tools).map((g) => (
                    <TableBody key={g.tool}>
                      <TableRow>
                        <th scope="rowgroup" colSpan={5} className="pt-4 pb-1 text-left text-sm font-medium">
                          {g.tool}
                        </th>
                      </TableRow>
                      {g.rows.map((t) => (
                        <TableRow key={`${t.tool}/${t.version}/${t.arch}`}>
                          <TableCell className="font-mono">{t.version}</TableCell>
                          <TableCell className="font-mono text-muted-foreground">{t.arch}</TableCell>
                          <TableCell>
                            <SizeBar bytes={t.bytes} longest={longest} />
                          </TableCell>
                          <TableCell className="font-mono">{dateTime(t.installed_at).slice(0, 10)}</TableCell>
                          <TableCell className="text-right">
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
                </Table>
              </div>
            )}
            {others.length > 0 && (
              <div className="flex flex-col gap-1">
                <h3 className="text-sm font-medium">Installed by jobs</h3>
                <p className="text-sm text-muted-foreground">A job's own setup step installed these. ghr does not manage them.</p>
                <ul className="flex flex-col gap-1 text-sm">
                  {others.map((f) => (
                    <li key={f.name} className="flex justify-between gap-4">
                      <span>{f.name}</span>
                      <span className="font-mono">{humanBytes(f.bytes)}</span>
                    </li>
                  ))}
                </ul>
              </div>
            )}
          </>
        )}
      </div>
      <ConfirmDialog confirm={confirm} onClose={() => setConfirm(null)} />
      <ConfirmDialog confirm={popular.confirm} onClose={popular.close} />
    </Section>
  )
}

const SLOT_CLASS: Record<CachePart["slot"], string> = {
  1: "bg-chart-1",
  2: "bg-chart-2",
  3: "bg-chart-3",
  4: "bg-chart-4",
  5: "bg-chart-5",
}

export function ToolCacheSection({ storage }: { storage: Storage }) {
  const parts = cacheParts(storage)
  const total = parts.reduce((n, p) => n + p.bytes, 0)
  if (total === 0) return null
  return (
    <Section title="Tool cache">
      <div className="flex flex-col gap-3">
        <div
          role="img"
          aria-label={`Tool cache: ${parts.map((p) => `${p.label} ${humanBytes(p.bytes)}`).join(", ")}`}
          className="flex h-3 overflow-hidden rounded-[3px] bg-muted"
        >
          {parts.map((p) => (
            <span key={p.key} data-part={p.key} className={SLOT_CLASS[p.slot]} style={{ width: `${(p.bytes / total) * 100}%` }} />
          ))}
        </div>
        <ul className="grid grid-cols-2 gap-x-4 gap-y-1 text-sm sm:grid-cols-3">
          {parts.map((p) => (
            <li key={p.key} className="flex items-center gap-2">
              <span aria-hidden="true" className={`size-2.5 shrink-0 rounded-[2px] ${SLOT_CLASS[p.slot]}`} />
              <span className="flex-1">{p.label}</span>
              <span className="font-mono text-muted-foreground">{humanBytes(p.bytes)}</span>
            </li>
          ))}
        </ul>
      </div>
    </Section>
  )
}
