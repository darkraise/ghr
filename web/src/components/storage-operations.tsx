import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "darkraise-ui/components/table"
import type { Deletion, GitHubOperation } from "@/api/types"
import { Section } from "@/components/page/section"
import { dateTime, humanBytes, shortWhen } from "@/lib/format"
import { useNow } from "@/lib/use-now"

const VERBS: Record<string, [running: string, finished: string]> = {
  "delete-artifacts": ["Deleting artifacts", "Deleted artifacts"],
  "delete-caches": ["Deleting caches", "Deleted caches"],
  "set-retention": ["Setting retention", "Set retention"],
}

export function OperationProgress({ operation }: { operation: GitHubOperation | undefined }) {
  const now = useNow()
  if (!operation) return null
  const running = operation.finished_at === undefined
  const [doing, done] = VERBS[operation.kind] ?? [operation.kind, operation.kind]
  const title = `${running ? doing : done}${operation.repo ? ` in ${operation.repo}` : " on every repository"}`
  const parts = [`${operation.done} of ${operation.total}`]
  if (operation.bytes > 0) parts.push(`${humanBytes(operation.bytes)} freed`)
  if (operation.failed.length > 0) parts.push(`${operation.failed.length} failed`)
  if (operation.finished_at) parts.push(`finished ${shortWhen(operation.finished_at, now)}`)
  return (
    <section aria-label="Operation" className="flex flex-col gap-2 rounded-md border border-border bg-card p-4">
      <p className="text-sm font-semibold">{title}</p>
      {running && (
        <div
          role="progressbar"
          aria-label={title}
          aria-valuemin={0}
          aria-valuemax={operation.total}
          aria-valuenow={operation.done}
          className="h-2 overflow-hidden rounded-[3px] bg-muted"
        >
          <span className="block h-full bg-primary" style={{ width: `${operation.total > 0 ? (operation.done / operation.total) * 100 : 0}%` }} />
        </div>
      )}
      <p className="text-sm text-muted-foreground">{parts.join(", ")}</p>
      {operation.failed.length > 0 && (
        <ul className="flex flex-col gap-0.5 text-sm text-destructive">
          {operation.failed.map((f) => (
            <li key={`${f.repo}-${f.id ?? ""}`}>{`${f.repo}${f.id ? ` ${f.id}` : ""}: ${f.error}`}</li>
          ))}
        </ul>
      )}
    </section>
  )
}

export function RecentDeletions({ deletions }: { deletions: Deletion[] }) {
  const now = useNow()
  return (
    <Section id="deletions" title="Recent deletions" flush>
      {deletions.length === 0 ? (
        <p className="p-4 text-sm text-muted-foreground">Nothing deleted since ghr started.</p>
      ) : (
        <div className="overflow-x-auto [&_.dr-table-wrapper]:border-0">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead className="pl-4">Time</TableHead>
                <TableHead>Repository</TableHead>
                <TableHead>Kind</TableHead>
                <TableHead>Name</TableHead>
                <TableHead className="text-right">Size</TableHead>
                <TableHead className="text-right">Age</TableHead>
                <TableHead className="pr-4">By</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {deletions.map((d, i) => (
                <TableRow key={`${d.at}-${d.repo}-${d.name}-${i}`}>
                  <TableCell className="pl-4 whitespace-nowrap text-muted-foreground">
                    <time dateTime={d.at} title={dateTime(d.at)}>
                      {shortWhen(d.at, now)}
                    </time>
                  </TableCell>
                  <TableCell>{d.repo}</TableCell>
                  <TableCell>{d.kind}</TableCell>
                  <TableCell className="font-mono text-xs break-all whitespace-normal">{d.name}</TableCell>
                  <TableCell className="text-right font-mono whitespace-nowrap">{humanBytes(d.bytes)}</TableCell>
                  <TableCell className="text-right font-mono">{d.age}</TableCell>
                  <TableCell className="pr-4">{d.by === "cleanup" ? "Cleanup" : "Manual"}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      )}
    </Section>
  )
}
