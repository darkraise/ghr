import { useMutation, useQueryClient } from "@tanstack/react-query"
import { Link } from "@tanstack/react-router"
import { Button } from "darkraise-ui/components/button"
import { Card, CardContent, CardHeader, CardTitle } from "darkraise-ui/components/card"
import { EmptyState } from "darkraise-ui/components/empty-state"
import { toast } from "darkraise-ui/components/sonner"
import { Spinner } from "darkraise-ui/components/spinner"
import { PageHeader } from "darkraise-ui/layout"
import { useState } from "react"
import { api } from "@/api/client"
import { keys, useConfig, useStatus } from "@/api/hooks"
import { AddRepoDialog } from "@/components/add-repo-dialog"
import { ConfirmDialog, type Confirm } from "@/components/confirm-dialog"
import { ActivitySummary, RepoSummary } from "@/components/repo-summary"
import { useNow } from "@/lib/use-now"

export function RepositoriesPage() {
  const status = useStatus()
  const config = useConfig()
  const now = useNow()
  const queryClient = useQueryClient()
  const [confirm, setConfirm] = useState<Confirm | null>(null)
  const [adding, setAdding] = useState(false)
  const refresh = () =>
    Promise.all([
      queryClient.invalidateQueries({ queryKey: keys.status }),
      queryClient.invalidateQueries({ queryKey: keys.config }),
    ])
  const pause = useMutation({
    mutationFn: ({ name, resume }: { name: string; resume: boolean }) => (resume ? api.resumeRepo(name) : api.pauseRepo(name)),
    onSuccess: (_data, { name, resume }) => {
      toast.success(`${resume ? "resumed" : "paused"} ${name}`)
    },
    onSettled: refresh,
  })
  const remove = useMutation({
    mutationFn: (name: string) => api.removeRepo(name),
    onSuccess: (_data, name) => {
      toast.success(`removing ${name}`)
    },
    onSettled: refresh,
  })

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
                  <Button
                    size="sm"
                    variant="secondary"
                    aria-label={`${r.paused ? "Resume" : "Pause"} ${r.name}`}
                    disabled={offline || Boolean(r.removing)}
                    onClick={() => pause.mutate({ name: r.name, resume: r.paused })}
                  >
                    {r.paused ? "Resume" : "Pause"}
                  </Button>
                  <Button
                    size="sm"
                    variant="destructive"
                    aria-label={`Remove ${r.name}`}
                    disabled={offline || Boolean(r.removing)}
                    onClick={() =>
                      setConfirm({
                        title: `Remove repo ${r.name}? Its running jobs finish first.`,
                        action: "Remove",
                        destructive: true,
                        run: () => remove.mutate(r.name),
                      })
                    }
                  >
                    Remove
                  </Button>
                </div>
              </CardContent>
            </Card>
          ))}
        </div>
      )}
      <ConfirmDialog confirm={confirm} onClose={() => setConfirm(null)} />
      <AddRepoDialog open={adding} onClose={() => setAdding(false)} />
    </>
  )
}
