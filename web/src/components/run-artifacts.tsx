import { useMutation } from "@tanstack/react-query"
import { Button } from "darkraise-ui/components/button"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "darkraise-ui/components/table"
import { api } from "@/api/client"
import type { RunArtifact } from "@/api/types"
import { Section } from "@/components/page/section"
import { dateTime, humanBytes } from "@/lib/format"

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

export function ArtifactsSection({ repo, artifacts }: { repo: string; artifacts: RunArtifact[] }) {
  if (artifacts.length === 0) return null
  return (
    <Section title="Artifacts">
      <div className="overflow-x-auto">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Name</TableHead>
              <TableHead className="text-right">Size</TableHead>
              <TableHead className="text-right">Expires</TableHead>
              <TableHead>
                <span className="sr-only">Download</span>
              </TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {artifacts.map((a) => (
              <TableRow key={a.id}>
                <TableCell className="font-mono">{a.name}</TableCell>
                <TableCell className="text-right font-mono">{humanBytes(a.size_bytes)}</TableCell>
                <TableCell className="text-right font-mono">{a.expires_at ? dateTime(a.expires_at).slice(0, 10) : ""}</TableCell>
                <TableCell className="text-right">
                  {a.expired ? <span className="text-sm text-muted-foreground">Expired</span> : <DownloadButton repo={repo} artifact={a} />}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </div>
    </Section>
  )
}
