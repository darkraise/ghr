import { useMutation, useQuery } from "@tanstack/react-query"
import { Button } from "darkraise-ui/components/button"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "darkraise-ui/components/table"
import { api } from "@/api/client"
import { keys } from "@/api/hooks"
import type { RunArtifact } from "@/api/types"
import { CopyLinkMenu, DownloadObject } from "@/components/artifact-store-items"
import { ErrorLine } from "@/components/page/error-line"
import { Section } from "@/components/page/section"
import { storeName } from "@/lib/artifact-store"
import { dateTime, humanBytes, plural } from "@/lib/format"
import { errorText } from "@/query"

function DownloadButton({ repo, artifact }: { repo: string; artifact: RunArtifact }) {
  // GitHub's signed address expires after about a minute, so every click asks
  // the daemon for a new one rather than keeping it.
  const download = useMutation({
    mutationFn: () => api.artifactDownload(repo, artifact.id),
    onSuccess: (d) => window.location.assign(d.url),
  })
  return (
    <Button size="sm" variant="outline" loading={download.isPending} aria-label={`Download ${artifact.name}`} onClick={() => download.mutate()}>
      Download
    </Button>
  )
}

function StoreChip({ store }: { store: string }) {
  return <span>{storeName(store)}</span>
}

const day = (iso: string | undefined) => (iso ? dateTime(iso).slice(0, 10) : "")

export function ArtifactsSection({
  repo,
  runId,
  artifacts,
  selfHosted,
}: {
  repo: string
  runId: number
  artifacts: RunArtifact[]
  selfHosted: boolean
}) {
  // Listed live rather than taken from the snapshot, so an upload shows before
  // the next 15-minute inventory.
  const objects = useQuery({
    queryKey: keys.artifactRunObjects(repo, runId),
    queryFn: ({ signal }) => api.artifactRunObjects(repo, runId, signal),
    enabled: selfHosted,
  })
  const items = selfHosted ? (objects.data ?? []) : []
  const failed = selfHosted && objects.isError
  const total = artifacts.length + items.length
  if (total === 0 && !failed) return null
  const aside = items.length > 0 && <span className="ml-auto text-sm text-muted-foreground">{`${plural(total, "artifact")} · ${items.length} on RustFS`}</span>
  return (
    <Section title="Artifacts" aside={aside}>
      <div className="flex flex-col gap-3">
        {failed && <ErrorLine onRetry={() => void objects.refetch()}>{`Could not list the RustFS artifacts: ${errorText(objects.error)}`}</ErrorLine>}
        {total > 0 && (
          <div className="overflow-x-auto">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Name</TableHead>
                  <TableHead>Store</TableHead>
                  <TableHead className="text-right">Size</TableHead>
                  <TableHead className="text-right">Expires</TableHead>
                  <TableHead>
                    <span className="sr-only">Actions</span>
                  </TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {items.map((i) => (
                  <TableRow key={i.key}>
                    <TableCell className="font-mono">{i.name}</TableCell>
                    <TableCell>
                      <StoreChip store="rustfs" />
                    </TableCell>
                    <TableCell className="text-right font-mono">{humanBytes(i.bytes)}</TableCell>
                    <TableCell className="text-right font-mono">{day(i.expires_at)}</TableCell>
                    <TableCell className="text-right">
                      <div className="flex items-center justify-end gap-1">
                        <DownloadObject item={i} label={i.name} />
                        <CopyLinkMenu item={i} label={i.name} />
                      </div>
                    </TableCell>
                  </TableRow>
                ))}
                {artifacts.map((a) => (
                  <TableRow key={a.id}>
                    <TableCell className="font-mono">{a.name}</TableCell>
                    <TableCell>
                      <StoreChip store="github" />
                    </TableCell>
                    <TableCell className="text-right font-mono">{humanBytes(a.size_bytes)}</TableCell>
                    <TableCell className="text-right font-mono">{day(a.expires_at)}</TableCell>
                    <TableCell className="text-right">
                      {a.expired ? <span className="text-sm text-muted-foreground">Expired</span> : <DownloadButton repo={repo} artifact={a} />}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
        )}
        {items.length > 0 && artifacts.length > 0 && (
          <p className="text-xs text-muted-foreground">
            GitHub artifacts here come from steps that still use actions/upload-artifact, or from before the repository was turned on.
          </p>
        )}
      </div>
    </Section>
  )
}
