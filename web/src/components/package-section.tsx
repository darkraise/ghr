import { useMutation, useQueryClient } from "@tanstack/react-query"
import { Button } from "darkraise-ui/components/button"
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "darkraise-ui/components/collapsible"
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "darkraise-ui/components/dropdown-menu"
import { Input } from "darkraise-ui/components/input"
import { toast } from "darkraise-ui/components/sonner"
import { Spinner } from "darkraise-ui/components/spinner"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "darkraise-ui/components/table"
import { Tooltip, TooltipContent, TooltipTrigger } from "darkraise-ui/components/tooltip"
import { ArrowUp, ChevronRight, CircleAlert, Ellipsis } from "lucide-react"
import { useState, type ReactNode } from "react"
import { ApiError, api } from "@/api/client"
import { keys, usePackages } from "@/api/hooks"
import type { PackageInfo, Status } from "@/api/types"
import { ConfirmDialog, type Confirm } from "@/components/confirm-dialog"
import { PackageAddDialog } from "@/components/package-add-dialog"
import { ErrorLine } from "@/components/page/error-line"
import { Section } from "@/components/page/section"
import { RefusedHint } from "@/components/refused-hint"
import { ago, dateTime, humanBytes } from "@/lib/format"
import { filterPackages, upgradable } from "@/lib/packages"
import { MAINTENANCE_HOLD } from "@/lib/storage"
import { useNow } from "@/lib/use-now"
import { errorText } from "@/query"

const TITLE = "System packages"
// The daemon also answers 503 while it shuts down.
const UNAVAILABLE = "apt is not available on this host"

interface PackageAction {
  call: () => Promise<unknown>
  done: string
}

// Errors toast from the query client's mutation cache.
function usePackageAction() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (a: PackageAction) => a.call(),
    onSuccess: (_data, a) => {
      toast.success(a.done)
    },
    onSettled: async () => {
      await Promise.all([queryClient.invalidateQueries({ queryKey: keys.packages }), queryClient.invalidateQueries({ queryKey: keys.storage })])
    },
  })
}

export function PackageSection({ status, offline }: { status: Status | undefined; offline: boolean }) {
  const packages = usePackages()
  const now = useNow()
  const action = usePackageAction()
  const [adding, setAdding] = useState(false)
  const [confirm, setConfirm] = useState<Confirm | null>(null)
  const run = (a: PackageAction) => action.mutate(a)
  const failed = packages.isError && <ErrorLine onRetry={() => void packages.refetch()}>{errorText(packages.error)}</ErrorLine>

  if (!packages.data) {
    if (packages.error instanceof ApiError && packages.error.status === 503 && packages.error.message === UNAVAILABLE) {
      return (
        <Section title={TITLE}>
          <p className="text-sm text-muted-foreground">{UNAVAILABLE}</p>
        </Section>
      )
    }
    return <Section title={TITLE}>{failed || <Spinner label="Loading packages" />}</Section>
  }

  const data = packages.data
  const managed = data.managed ?? []
  const pending = upgradable(data)
  const updated = data.index_updated_at

  function askUninstall(p: PackageInfo) {
    setConfirm({
      title: `Uninstall ${p.name}?`,
      body: `ghr removes ${p.name} with dpkg, which refuses while another installed package depends on it, and drops it from the managed list. ${MAINTENANCE_HOLD}`,
      action: "Uninstall",
      destructive: true,
      run: () => run({ call: () => api.uninstallPackage(p.name), done: `Queued: uninstall ${p.name}` }),
    })
  }

  const aside: ReactNode = (
    <>
      {updated && (
        <span className="text-sm text-muted-foreground">
          index updated{" "}
          <time dateTime={updated} title={dateTime(updated)}>
            {ago(now - Date.parse(updated))}
          </time>
        </span>
      )}
      <RefusedHint what="Upgrade and uninstall" status={status} />
      <div className="ml-auto flex flex-wrap gap-2">
        <Button size="sm" variant="outline" disabled={offline} onClick={() => run({ call: () => api.refreshPackageIndex(), done: "Queued: refresh apt index" })}>
          Refresh index
        </Button>
        {pending.length > 0 && (
          <Button
            size="sm"
            variant="outline"
            disabled={offline}
            onClick={() => run({ call: () => api.upgradePackages({ all: true }), done: "Queued: upgrade all managed packages" })}
          >
            {`Upgrade all (${pending.length})`}
          </Button>
        )}
        <Button size="sm" disabled={offline} onClick={() => setAdding(true)}>
          Add package
        </Button>
      </div>
    </>
  )

  return (
    <Section title={TITLE} aside={aside} flush>
      {failed && <div className="p-4">{failed}</div>}
      {managed.length === 0 ? (
        <p className="p-4 text-sm text-muted-foreground">ghr manages no apt packages yet</p>
      ) : (
        // The card is the frame, so the table drops its own border.
        <div className="overflow-x-auto [&_.dr-table-wrapper]:border-0">
          <Table aria-label="Managed packages">
            <TableHeader>
              <TableRow>
                <TableHead className="pl-4">Package</TableHead>
                <TableHead>Version</TableHead>
                <TableHead className="text-right">Size</TableHead>
                <TableHead className="pr-4">
                  <span className="sr-only">Actions</span>
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {managed.map((p) => (
                <TableRow key={p.name}>
                  <TableCell className="pl-4">
                    <div className="font-mono">{p.name}</div>
                    {p.error && (
                      <div className="flex items-center gap-1.5 text-sm text-destructive">
                        <CircleAlert size={13} aria-hidden="true" className="shrink-0" />
                        {p.error}
                      </div>
                    )}
                  </TableCell>
                  <TableCell>
                    <span className="flex flex-wrap items-center gap-2">
                      {p.installed ? <span className="font-mono">{p.version}</span> : <span className="text-muted-foreground">not installed</span>}
                      {p.upgradable && p.candidate && (
                        <span className="inline-flex items-center gap-1 rounded-full bg-muted px-2 py-0.5 text-xs font-semibold whitespace-nowrap">
                          <ArrowUp size={13} aria-hidden="true" className="shrink-0" />
                          {`Upgrade to ${p.candidate}`}
                        </span>
                      )}
                    </span>
                  </TableCell>
                  <TableCell className="text-right font-mono">{p.installed ? humanBytes(p.bytes) : ""}</TableCell>
                  <TableCell className="pr-4 text-right">
                    <DropdownMenu>
                      <Tooltip>
                        <TooltipTrigger asChild>
                          <DropdownMenuTrigger asChild>
                            <Button size="icon" variant="ghost" aria-label={`More actions for ${p.name}`} disabled={offline}>
                              <Ellipsis size={15} aria-hidden="true" />
                            </Button>
                          </DropdownMenuTrigger>
                        </TooltipTrigger>
                        <TooltipContent>More actions</TooltipContent>
                      </Tooltip>
                      <DropdownMenuContent align="end">
                        <DropdownMenuItem
                          disabled={!p.upgradable}
                          onSelect={() => run({ call: () => api.upgradePackages({ names: [p.name] }), done: `Queued: upgrade ${p.name}` })}
                        >
                          Upgrade
                        </DropdownMenuItem>
                        <DropdownMenuItem
                          onSelect={() => run({ call: () => api.forgetPackage(p.name), done: `${p.name} is no longer managed; it stays installed` })}
                        >
                          Forget
                        </DropdownMenuItem>
                        <DropdownMenuItem disabled={!p.installed} onSelect={() => askUninstall(p)} className="text-destructive">
                          Uninstall
                        </DropdownMenuItem>
                      </DropdownMenuContent>
                    </DropdownMenu>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      )}
      <BaseList
        list={data.base ?? []}
        offline={offline}
        onForget={(p) => run({ call: () => api.forgetPackage(p.name), done: `${p.name} is no longer managed; it stays installed` })}
      />
      <DetectedList list={data.detected ?? []} offline={offline} onAdd={(p) => run({ call: () => api.addPackages([p.name]), done: `Queued: adopt ${p.name}` })} />
      <ConfirmDialog confirm={confirm} onClose={() => setConfirm(null)} />
      <PackageAddDialog open={adding} onClose={() => setAdding(false)} />
    </Section>
  )
}

function Fold({ title, children }: { title: string; children: ReactNode }) {
  return (
    <Collapsible className="group border-t border-border">
      <CollapsibleTrigger className="flex w-full items-center gap-2 px-4 py-2.5 text-left text-sm font-semibold hover:bg-muted/40">
        <ChevronRight size={15} aria-hidden="true" className="shrink-0 transition-transform group-data-[state=open]:rotate-90" />
        {title}
      </CollapsibleTrigger>
      <CollapsibleContent className="px-4 pb-4">{children}</CollapsibleContent>
    </Collapsible>
  )
}

// A managed name that is base on this host (a config copied from another
// release) can only be forgotten.
function BaseList({ list, offline, onForget }: { list: PackageInfo[]; offline: boolean; onForget: (p: PackageInfo) => void }) {
  return (
    <Fold title={`Base packages (${list.length})`}>
      <p className="mb-2 text-sm text-muted-foreground">setup.sh installs these, so ghr does not change them.</p>
      <ul aria-label="Base packages" className="flex flex-col gap-1 text-sm">
        {list.map((p) => (
          <li key={p.name} className="flex items-center gap-2">
            <span className="font-mono">{p.name}</span>
            <span className="font-mono text-muted-foreground">{p.version}</span>
            {p.managed && (
              <Button size="sm" variant="outline" className="ml-auto" aria-label={`Forget ${p.name}`} disabled={offline} onClick={() => onForget(p)}>
                Forget
              </Button>
            )}
          </li>
        ))}
      </ul>
    </Fold>
  )
}

function DetectedList({ list, offline, onAdd }: { list: PackageInfo[]; offline: boolean; onAdd: (p: PackageInfo) => void }) {
  const [filter, setFilter] = useState("")
  const shown = filterPackages(list, filter)
  return (
    <Fold title={`Installed, not managed (${list.length})`}>
      <p className="mb-2 text-sm text-muted-foreground">Installed by hand. Adding one puts it on the managed list and installs nothing.</p>
      <Input aria-label="Filter installed packages" placeholder="Type to filter" className="mb-2" value={filter} onChange={(e) => setFilter(e.target.value)} />
      {shown.length === 0 ? (
        <p className="text-sm text-muted-foreground">{list.length === 0 ? "Nothing to add" : "No match"}</p>
      ) : (
        <ul aria-label="Installed, not managed" className="flex flex-col gap-1 text-sm">
          {shown.map((p) => (
            <li key={p.name} className="flex items-center gap-2">
              <span className="font-mono">{p.name}</span>
              <span className="font-mono text-muted-foreground">{p.version}</span>
              <Button size="sm" variant="outline" className="ml-auto" aria-label={`Add ${p.name}`} disabled={offline} onClick={() => onAdd(p)}>
                Add
              </Button>
            </li>
          ))}
        </ul>
      )}
    </Fold>
  )
}
