import { useMutation, useQueryClient } from "@tanstack/react-query"
import { Link } from "@tanstack/react-router"
import { Button } from "darkraise-ui/components/button"
import { toast } from "darkraise-ui/components/sonner"
import { Spinner } from "darkraise-ui/components/spinner"
import { PageHeader } from "darkraise-ui/layout"
import { api } from "@/api/client"
import { keys, useConfig, useGitHubStorage, useStatus } from "@/api/hooks"
import type { Config } from "@/api/types"
import { ArtifactRepoTable } from "@/components/artifact-repo-table"
import { ErrorLine } from "@/components/page/error-line"
import { QuotaCard } from "@/components/quota-card"
import { OperationProgress, RecentDeletions } from "@/components/storage-operations"
import { shortWhen } from "@/lib/format"
import { useNow } from "@/lib/use-now"
import { errorText } from "@/query"

function policySummary(config: Config | undefined): string {
  if (!config) return ""
  const g = config.github_storage
  const [warn, critical] = g?.warn_pct ?? [70, 90]
  const n = Object.keys(g?.repos ?? {}).length
  const overrides = n === 0 ? "" : ` (${n} repository override${n === 1 ? "" : "s"})`
  return ` · cleanup ${g?.cleanup_max_age || "off"}${overrides} · warnings at ${warn}% and ${critical}%`
}

export function ArtifactsPage() {
  const storage = useGitHubStorage()
  const status = useStatus()
  const config = useConfig()
  const queryClient = useQueryClient()
  const now = useNow()
  const refresh = useMutation({
    mutationFn: () => api.refreshGitHubStorage(),
    onSuccess: () => {
      toast.success("Check started")
    },
    onSettled: () => queryClient.invalidateQueries({ queryKey: keys.githubStorage }),
  })
  const data = storage.data
  const offline = status.isError
  const checked = data ? (data.checked_at ? `Checked ${shortWhen(data.checked_at, now)}` : "Not checked yet") : undefined
  return (
    <div className="flex flex-col gap-4">
      <PageHeader
        title="Artifacts"
        description={checked && `${checked}${policySummary(config.data)}`}
        actions={
          <>
            <Button variant="outline" asChild>
              <Link to="/settings/$section" params={{ section: "github-storage" }}>
                Policy
              </Link>
            </Button>
            <Button variant="outline" disabled={offline || refresh.isPending} onClick={() => refresh.mutate()}>
              Check now
            </Button>
          </>
        }
      />
      {storage.isError && <ErrorLine onRetry={() => void storage.refetch()}>{errorText(storage.error)}</ErrorLine>}
      {data?.error && <ErrorLine>{`Could not list the repositories: ${data.error}`}</ErrorLine>}
      {!data && !storage.isError && <Spinner label="Loading" />}
      {data && <QuotaCard storage={data} />}
      {data && <OperationProgress operation={data.operation} />}
      {data && <ArtifactRepoTable storage={data} config={config.data} offline={offline} />}
      {data && <RecentDeletions deletions={data.deletions} />}
    </div>
  )
}
