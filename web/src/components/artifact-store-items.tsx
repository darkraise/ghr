import { useMutation, useQueryClient } from "@tanstack/react-query"
import { Link } from "@tanstack/react-router"
import { Button } from "darkraise-ui/components/button"
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuLabel, DropdownMenuTrigger } from "darkraise-ui/components/dropdown-menu"
import { Input } from "darkraise-ui/components/input"
import { toast } from "darkraise-ui/components/sonner"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "darkraise-ui/components/table"
import { Tooltip, TooltipContent, TooltipTrigger } from "darkraise-ui/components/tooltip"
import { ChevronDown, Trash2 } from "lucide-react"
import { useState } from "react"
import { api } from "@/api/client"
import { keys } from "@/api/hooks"
import type { ArtifactLinkTTL, ArtifactStoreItem, ArtifactStoreRepo } from "@/api/types"
import type { Confirm } from "@/components/confirm-dialog"
import { expiryText, LINK_TTLS, olderThan } from "@/lib/artifact-store"
import { copyText } from "@/lib/clipboard"
import { ago, dateTime, humanBytes, plural } from "@/lib/format"
import { useNow } from "@/lib/use-now"

const NUM = "text-right font-mono whitespace-nowrap"
const WIDE = "hidden sm:table-cell"

const LAN_ONLY = "Works only on the store's network"

// ghr accepts http:// only for a private, loopback or link-local address, so
// its links reach the store only from that network.
const lanOnly = (endpoint: string | undefined) => /^http:\/\//i.test(endpoint ?? "")

// The link is signed for one hour and asked for on each click, so a page left
// open never hands the browser an expired one.
export function DownloadObject({ item, label, lan = false }: { item: ArtifactStoreItem; label: string; lan?: boolean }) {
  const download = useMutation({
    mutationFn: () => api.artifactLink(item.key, "1h"),
    onSuccess: (link) => window.location.assign(link.url),
  })
  const button = (
    <Button size="sm" variant="outline" loading={download.isPending} aria-label={`Download ${label}`} onClick={() => download.mutate()}>
      Download
    </Button>
  )
  if (!lan) return button
  return (
    <Tooltip>
      <TooltipTrigger asChild>{button}</TooltipTrigger>
      <TooltipContent>{LAN_ONLY}</TooltipContent>
    </Tooltip>
  )
}

export function CopyLinkMenu({ item, label, lan = false }: { item: ArtifactStoreItem; label: string; lan?: boolean }) {
  const copy = useMutation({
    mutationFn: async (ttl: ArtifactLinkTTL) => {
      const link = await api.artifactLink(item.key, ttl)
      await copyText(link.url)
    },
    onSuccess: (_d, ttl) => toast.success(`Link copied · valid for ${LINK_TTLS.find((t) => t.ttl === ttl)?.label ?? ttl}`),
  })
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button size="sm" variant="outline" disabled={copy.isPending} aria-label={`Copy a link to ${label}`}>
          Copy link
          <ChevronDown size={14} aria-hidden="true" />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="max-w-64">
        <DropdownMenuLabel>Link valid for</DropdownMenuLabel>
        {LINK_TTLS.map((t) => (
          <DropdownMenuItem key={t.ttl} onSelect={() => copy.mutate(t.ttl)}>
            {t.label}
          </DropdownMenuItem>
        ))}
        <p className="px-2 py-1.5 text-xs text-muted-foreground">Anyone with the link can download it until it expires. The link goes straight to RustFS.</p>
        {lan && <p className="px-2 pb-1.5 text-xs text-muted-foreground">{`${LAN_ONLY}.`}</p>}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

export function ArtifactStoreItems({
  repo,
  endpoint,
  offline,
  busy,
  ask,
}: {
  repo: ArtifactStoreRepo
  endpoint: string | undefined
  offline: boolean
  busy: boolean
  ask: (c: Confirm) => void
}) {
  const queryClient = useQueryClient()
  const now = useNow()
  const [older, setOlder] = useState("")
  const remove = useMutation({
    mutationFn: (objectKeys: string[]) => api.deleteArtifactObjects(repo.name, objectKeys),
    onSuccess: () => {
      toast.success("Delete started")
      setOlder("")
    },
    onSettled: () => queryClient.invalidateQueries({ queryKey: keys.artifactStore }),
  })
  const held = offline || busy || remove.isPending
  const lan = lanOnly(endpoint)

  function askOlder() {
    const age = older.trim()
    const old = olderThan(repo.items, age, now)
    if (old === null) {
      toast.error("Enter an age such as 7d or 12h")
      return
    }
    if (old.length === 0) {
      toast(`No artifact of ${repo.name} is older than ${age}`)
      return
    }
    const bytes = old.reduce((n, i) => n + i.bytes, 0)
    ask({
      title: `Delete ${repo.name}'s artifacts older than ${age}?`,
      body: `${plural(old.length, "artifact")} (${humanBytes(bytes)}) on RustFS. A run that downloads them later fails.`,
      action: "Delete",
      destructive: true,
      run: () => remove.mutate(old.map((i) => i.key)),
    })
  }

  return (
    <section aria-label={`Artifacts of ${repo.name}`} className="flex min-w-0 flex-col gap-2 px-4 py-3">
      <div className="flex flex-wrap items-center gap-2">
        <h3 className="text-sm font-semibold">Artifacts</h3>
        <div className="ml-auto flex flex-wrap items-center gap-2">
          <Input
            aria-label={`Delete artifacts of ${repo.name} older than`}
            className="w-20 font-mono"
            placeholder="7d"
            value={older}
            disabled={offline}
            onChange={(e) => setOlder(e.target.value)}
          />
          <Button size="sm" variant="outline" disabled={held || older.trim() === ""} onClick={askOlder}>
            Delete older
          </Button>
        </div>
      </div>
      {repo.items.length === 0 ? (
        <p className="text-sm text-muted-foreground">No artifacts on RustFS.</p>
      ) : (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Name</TableHead>
              <TableHead>Run</TableHead>
              <TableHead className={WIDE}>Branch</TableHead>
              <TableHead className="text-right">Size</TableHead>
              <TableHead className={WIDE}>Uploaded</TableHead>
              <TableHead className={WIDE}>Expires</TableHead>
              <TableHead>
                <span className="sr-only">Actions</span>
              </TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {repo.items.map((i) => {
              const label = `${i.name} from run ${i.run_id}`
              return (
                <TableRow key={i.key}>
                  <th scope="row" className="py-2 text-left font-mono font-normal">
                    {i.name}
                  </th>
                  <TableCell className="whitespace-nowrap">
                    <Link to="/actions/$repo/runs/$id" params={{ repo: repo.name, id: String(i.run_id) }} className="hover:text-primary hover:underline">
                      {`Run ${i.run_id}`}
                    </Link>
                  </TableCell>
                  <TableCell className={`font-mono text-xs ${WIDE}`}>{i.branch ?? ""}</TableCell>
                  <TableCell className={NUM}>{humanBytes(i.bytes)}</TableCell>
                  <TableCell className={`whitespace-nowrap text-muted-foreground ${WIDE}`}>
                    <time dateTime={i.uploaded_at} title={dateTime(i.uploaded_at)}>
                      {ago(now - Date.parse(i.uploaded_at))}
                    </time>
                  </TableCell>
                  <TableCell className={`font-mono whitespace-nowrap ${WIDE}`}>{expiryText(i, repo.cleanup)}</TableCell>
                  <TableCell>
                    <div className="flex items-center justify-end gap-1">
                      <DownloadObject item={i} label={label} lan={lan} />
                      <CopyLinkMenu item={i} label={label} lan={lan} />
                      <Button
                        size="icon"
                        variant="ghost"
                        aria-label={`Delete ${label}`}
                        disabled={held}
                        onClick={() =>
                          ask({
                            title: `Delete ${label}?`,
                            body: `Frees ${humanBytes(i.bytes)} on RustFS. A run that downloads it later fails.`,
                            action: "Delete",
                            destructive: true,
                            run: () => remove.mutate([i.key]),
                          })
                        }
                      >
                        <Trash2 size={15} aria-hidden="true" />
                      </Button>
                    </div>
                  </TableCell>
                </TableRow>
              )
            })}
          </TableBody>
        </Table>
      )}
      {repo.count > repo.items.length && <p className="text-xs text-muted-foreground">{`Showing the newest ${repo.items.length} of ${repo.count}.`}</p>}
    </section>
  )
}
