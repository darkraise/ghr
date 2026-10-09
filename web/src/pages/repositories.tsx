import { useMutation, useQueryClient } from "@tanstack/react-query"
import { useLocation, useNavigate } from "@tanstack/react-router"
import { Button } from "darkraise-ui/components/button"
import { toast } from "darkraise-ui/components/sonner"
import { Spinner } from "darkraise-ui/components/spinner"
import { Tooltip, TooltipContent, TooltipTrigger } from "darkraise-ui/components/tooltip"
import { PageHeader } from "darkraise-ui/layout"
import { X } from "lucide-react"
import { type ReactNode, useEffect, useState } from "react"
import { api } from "@/api/client"
import { keys, useActivity, useConfig, useStatus } from "@/api/hooks"
import { AddRepoDialog } from "@/components/add-repo-dialog"
import { ErrorLine } from "@/components/page/error-line"
import { Section } from "@/components/page/section"
import { RepoTable } from "@/components/repo-table"
import { WatchRepoDialog } from "@/components/watch-repo-dialog"
import { reposSummary } from "@/lib/repos"
import { useNow } from "@/lib/use-now"
import { errorText } from "@/query"

function WatchedSection({
  names,
  error,
  offline,
  onWatch,
}: {
  names: string[]
  error?: ReactNode
  offline: boolean
  onWatch: () => void
}) {
  const queryClient = useQueryClient()
  const remove = useMutation({
    mutationFn: (name: string) => api.unwatchRepo(name),
    onSuccess: (_data, name) => {
      toast.success(`Stopped watching ${name}`)
      void queryClient.invalidateQueries({ queryKey: keys.config })
      void queryClient.invalidateQueries({ queryKey: keys.actions })
    },
  })
  return (
    <Section
      id="watched"
      title="Watched repositories"
      aside={
        <Button size="sm" variant="outline" disabled={offline} onClick={onWatch}>
          Watch a repository
        </Button>
      }
    >
      <p className="text-sm text-muted-foreground">Their workflow runs show on the Actions page. ghr runs no runners for them.</p>
      {error}
      {names.length > 0 && (
        <ul className="mt-3 flex flex-col divide-y divide-border">
          {names.map((name) => (
            <li key={name} className="flex items-center justify-between gap-2 py-1.5">
              <span className="font-mono text-sm">{name}</span>
              <Tooltip>
                <TooltipTrigger asChild>
                  <Button
                    size="icon"
                    variant="ghost"
                    aria-label={`Stop watching ${name}`}
                    disabled={offline || remove.isPending}
                    onClick={() => remove.mutate(name)}
                  >
                    <X size={15} aria-hidden="true" />
                  </Button>
                </TooltipTrigger>
                <TooltipContent>Stop watching</TooltipContent>
              </Tooltip>
            </li>
          ))}
        </ul>
      )}
    </Section>
  )
}

export function RepositoriesPage() {
  const status = useStatus()
  const config = useConfig()
  const activity = useActivity("24h")
  const now = useNow()
  const navigate = useNavigate()
  const hash = useLocation({ select: (l) => l.hash })
  const [adding, setAdding] = useState(false)
  const [watching, setWatching] = useState(false)

  const st = status.data
  const watchedNames = config.data?.watch_repos
  const ready = st !== undefined && (config.data !== undefined || config.isError)
  // Scrolled here rather than by the router, so a link to #watched works
  // whatever the router does with hashes.
  useEffect(() => {
    if (ready && hash.replace(/^#/, "") === "watched") document.getElementById("watched")?.scrollIntoView()
  }, [ready, hash])

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
      {(config.data || config.isError) && (
        <WatchedSection
          names={watchedNames ?? []}
          error={
            config.isError && <ErrorLine onRetry={() => void config.refetch()}>{errorText(config.error)}</ErrorLine>
          }
          offline={offline}
          onWatch={() => setWatching(true)}
        />
      )}
      <AddRepoDialog open={adding} onClose={() => setAdding(false)} />
      <WatchRepoDialog open={watching} onClose={() => setWatching(false)} />
    </div>
  )
}
