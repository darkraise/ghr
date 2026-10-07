import { Link } from "@tanstack/react-router"
import { Button } from "darkraise-ui/components/button"
import { Card, CardContent, CardHeader, CardTitle } from "darkraise-ui/components/card"
import { EmptyState } from "darkraise-ui/components/empty-state"
import { Spinner } from "darkraise-ui/components/spinner"
import { PageHeader } from "darkraise-ui/layout"
import { useState } from "react"
import { useConfig, useStatus } from "@/api/hooks"
import { AddRepoDialog } from "@/components/add-repo-dialog"
import { RepoActionButtons } from "@/components/repo-actions"
import { ActivitySummary, RepoSummary } from "@/components/repo-summary"
import { useNow } from "@/lib/use-now"

export function RepositoriesPage() {
  const status = useStatus()
  const config = useConfig()
  const now = useNow()
  const [adding, setAdding] = useState(false)

  const st = status.data
  if (!st) {
    return (
      <>
        <PageHeader title="Repositories" />
        <Spinner label="waiting for the daemon…" />
      </>
    )
  }
  const offline = status.isError
  const addButton = (
    <Button disabled={offline} onClick={() => setAdding(true)}>
      + Add repository
    </Button>
  )
  return (
    <>
      <PageHeader title="Repositories" actions={addButton} />
      {st.repos.length === 0 ? (
        <EmptyState title="No repositories yet" action={addButton} />
      ) : (
        <div className="grid gap-4 lg:grid-cols-2">
          {st.repos.map((r) => (
            <Card key={r.name}>
              <CardHeader>
                <CardTitle>{r.name}</CardTitle>
              </CardHeader>
              <CardContent className="flex flex-col gap-3">
                <RepoSummary repo={r} now={now} />
                <ActivitySummary name={r.name} retention={config.data?.history_retention} now={now} />
                <div className="flex flex-wrap gap-2">
                  <Button size="sm" variant="outline" asChild>
                    <Link to="/repositories/$name" params={{ name: r.name }} aria-label={`Manage ${r.name}`}>
                      Manage
                    </Link>
                  </Button>
                  <RepoActionButtons repo={r} offline={offline} />
                </div>
              </CardContent>
            </Card>
          ))}
        </div>
      )}
      <AddRepoDialog open={adding} onClose={() => setAdding(false)} />
    </>
  )
}
