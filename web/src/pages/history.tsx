import { Card, CardContent } from "darkraise-ui/components/card"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "darkraise-ui/components/select"
import { Spinner } from "darkraise-ui/components/spinner"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "darkraise-ui/components/table"
import { PageHeader } from "darkraise-ui/layout"
import { useState } from "react"
import { useConfig, useHistory } from "@/api/hooks"
import { StateBadge } from "@/components/state-badge"
import { dateTime, dur } from "@/lib/format"
import { errorText } from "@/query"

const ALL = "__all__"
const RESULTS = ["success", "failure", "cancelled"]

export function HistoryPage() {
  const [repo, setRepo] = useState("")
  const [conclusion, setConclusion] = useState("")
  const config = useConfig()
  const history = useHistory(repo, conclusion)

  const names = (config.data?.repos ?? []).map((r) => r.name)
  const repos = repo && !names.includes(repo) ? [...names, repo] : names
  const rows = history.data ?? []
  const longest = Math.max(1, ...rows.map((h) => Date.parse(h.finished_at) - Date.parse(h.started_at)))

  return (
    <>
      <PageHeader title="History" />
      <div className="mb-4 flex flex-wrap gap-4">
        <Select value={repo || ALL} onValueChange={(v) => setRepo(v === ALL ? "" : v)}>
          <SelectTrigger aria-label="Repo" className="w-48">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value={ALL}>all repos</SelectItem>
            {repos.map((name) => (
              <SelectItem key={name} value={name}>
                {name}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Select value={conclusion || ALL} onValueChange={(v) => setConclusion(v === ALL ? "" : v)}>
          <SelectTrigger aria-label="Result" className="w-40">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value={ALL}>all results</SelectItem>
            {RESULTS.map((r) => (
              <SelectItem key={r} value={r}>
                {r}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>
      <Card>
        <CardContent className="p-4">
          {history.isError && <p className="mb-2 text-sm text-destructive">{errorText(history.error)}</p>}
          {history.isPending ? (
            <Spinner label="loading…" />
          ) : rows.length === 0 && !history.isError ? (
            <p className="py-6 text-center text-sm text-muted-foreground">no finished jobs yet</p>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Finished</TableHead>
                  <TableHead>Repo</TableHead>
                  <TableHead>Run</TableHead>
                  <TableHead>Job</TableHead>
                  <TableHead>Result</TableHead>
                  <TableHead>Duration</TableHead>
                  <TableHead />
                </TableRow>
              </TableHeader>
              <TableBody>
                {rows.map((h) => {
                  const took = Date.parse(h.finished_at) - Date.parse(h.started_at)
                  return (
                    <TableRow key={h.id}>
                      <TableCell>{dateTime(h.finished_at)}</TableCell>
                      <TableCell>{h.repo}</TableCell>
                      <TableCell>#{h.run_number}</TableCell>
                      <TableCell>{h.job_name}</TableCell>
                      <TableCell>
                        <StateBadge state={h.conclusion} />
                      </TableCell>
                      <TableCell>
                        <div className="flex items-center gap-2">
                          <span className="w-14">{dur(took)}</span>
                          <div className="h-1.5 w-24 rounded bg-muted">
                            <div className="h-1.5 rounded bg-primary" style={{ width: `${Math.round((took / longest) * 100)}%` }} />
                          </div>
                        </div>
                      </TableCell>
                      <TableCell className="text-right">
                        {h.html_url && (
                          <a href={h.html_url} target="_blank" rel="noreferrer" className="text-sm hover:underline">
                            Open run
                          </a>
                        )}
                      </TableCell>
                    </TableRow>
                  )
                })}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>
    </>
  )
}
