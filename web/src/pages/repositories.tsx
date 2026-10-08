import { useNavigate } from "@tanstack/react-router"
import { Button } from "darkraise-ui/components/button"
import { Spinner } from "darkraise-ui/components/spinner"
import { PageHeader } from "darkraise-ui/layout"
import { useState } from "react"
import { useActivity, useConfig, useStatus } from "@/api/hooks"
import { AddRepoDialog } from "@/components/add-repo-dialog"
import { Section } from "@/components/page/section"
import { RepoTable } from "@/components/repo-table"
import { reposSummary } from "@/lib/repos"
import { useNow } from "@/lib/use-now"

export function RepositoriesPage() {
  const status = useStatus()
  const config = useConfig()
  const activity = useActivity("24h")
  const now = useNow()
  const navigate = useNavigate()
  const [adding, setAdding] = useState(false)

  const st = status.data
  if (!st) {
    return (
      <div className="flex flex-col gap-4">
        <PageHeader title="Repositories" />
        <Spinner label="Waiting for the daemon" />
      </div>
    )
  }
  const offline = status.isError
  const add = (
    <Button disabled={offline} onClick={() => setAdding(true)}>
      Add repository
    </Button>
  )
  return (
    <div className="flex flex-col gap-4">
      <PageHeader title="Repositories" description={reposSummary(st) || undefined} actions={add} />
      {st.repos.length === 0 ? (
        <div className="flex flex-col items-start gap-2">
          <p className="text-sm text-muted-foreground">No repositories yet</p>
          {add}
        </div>
      ) : (
        <Section title="Configured repositories">
          <div className="overflow-x-auto">
            <RepoTable
              repos={st.repos}
              activity={activity.data?.repos}
              now={now}
              offline={offline}
              onOpen={(name) => void navigate({ to: "/repositories/$name", params: { name } })}
              columns="full"
              config={config.data}
            />
          </div>
        </Section>
      )}
      <AddRepoDialog open={adding} onClose={() => setAdding(false)} />
    </div>
  )
}
